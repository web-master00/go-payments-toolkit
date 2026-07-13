/*
# Exchange Order Book

## What it is

Go slice mechanics for deleting filled orders without retaining dead memory:

- **Slice headers** - A slice is a view `(ptr, len, cap)` over an underlying
  array. Reslicing with `orders = orders[n:]` advances `ptr` and shrinks `len`,
  but the backing array still holds the discarded prefix until those slots are
  cleared or the array is unreachable.
- **Efficient delete** - Zero the freed prefix (`orders[i] = Order{}`) before
  returning `orders[n:]`. That drops references (IDs/strings, and any future
  pointer fields) so the garbage collector can reclaim them instead of keeping
  filled orders alive through the backing store.
- **Why not just append-delete** - `append(s[:i], s[i+1:]...)` is fine for
  mid-slice removal; for consuming the front of a book (common in matching),
  prefix reslice + zeroing avoids shifting the entire opposite side on every fill.

## What it is used for

Building high-frequency trading (HFT) matching engines where garbage collection
latency can cost millions:

- **Matching loops** - Continuously remove fully filled resting orders under
  tight latency budgets.
- **GC pressure** - Retained order objects inflate heap live set and trigger
  longer STW / concurrent mark work at the worst times.
- **Deterministic cleanup** - Explicitly releasing slice slots keeps memory
  behavior predictable when millions of orders churn per day.
*/

package main

import (
	"fmt"
	"strings"
)

type Side int

const (
	Buy Side = iota
	Sell
)

func (s Side) String() string {
	if s == Buy {
		return "BUY"
	}
	return "SELL"
}

// Order is a resting or incoming order. Prices/quantities are int64 only.
type Order struct {
	ID         string
	Side       Side
	PriceCents int64
	Quantity   int64
}

// Fill records one match against a resting order.
type Fill struct {
	RestingID  string
	PriceCents int64
	Quantity   int64
}

// OrderBook keeps Bids (best/highest first) and Asks (best/lowest first).
type OrderBook struct {
	Bids   []Order
	Asks   []Order
	nextID int
}

func NewOrderBook() *OrderBook {
	return &OrderBook{nextID: 1}
}

func (ob *OrderBook) newID(prefix string) string {
	id := fmt.Sprintf("%s-%d", prefix, ob.nextID)
	ob.nextID++
	return id
}

// AddLimitOrder inserts a resting limit order, preserving sort order.
func (ob *OrderBook) AddLimitOrder(side Side, priceCents, qty int64) Order {
	o := Order{
		ID:         ob.newID("L"),
		Side:       side,
		PriceCents: priceCents,
		Quantity:   qty,
	}
	if side == Buy {
		ob.Bids = insertBid(ob.Bids, o)
	} else {
		ob.Asks = insertAsk(ob.Asks, o)
	}
	return o
}

func insertBid(bids []Order, o Order) []Order {
	i := 0
	for i < len(bids) && bids[i].PriceCents >= o.PriceCents {
		i++
	}
	bids = append(bids, Order{})
	copy(bids[i+1:], bids[i:])
	bids[i] = o
	return bids
}

func insertAsk(asks []Order, o Order) []Order {
	i := 0
	for i < len(asks) && asks[i].PriceCents <= o.PriceCents {
		i++
	}
	asks = append(asks, Order{})
	copy(asks[i+1:], asks[i:])
	asks[i] = o
	return asks
}

// removePrefix drops the first n orders, zeroing freed backing-array slots.
func removePrefix(orders []Order, n int) []Order {
	if n <= 0 {
		return orders
	}
	if n > len(orders) {
		n = len(orders)
	}
	for i := 0; i < n; i++ {
		orders[i] = Order{} // release references before reslice
	}
	return orders[n:]
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// MatchOrder fills a market order against the opposite side of the book.
func (ob *OrderBook) MatchOrder(incoming Order) (fills []Fill, residual Order) {
	residual = incoming
	if incoming.Quantity <= 0 {
		return nil, residual
	}

	var book *[]Order
	if incoming.Side == Buy {
		book = &ob.Asks
	} else {
		book = &ob.Bids
	}

	filledPrefix := 0
	for i := 0; i < len(*book) && residual.Quantity > 0; i++ {
		resting := &(*book)[i]
		traded := min64(residual.Quantity, resting.Quantity)
		fills = append(fills, Fill{
			RestingID:  resting.ID,
			PriceCents: resting.PriceCents,
			Quantity:   traded,
		})
		resting.Quantity -= traded
		residual.Quantity -= traded
		if resting.Quantity == 0 {
			filledPrefix++
		}
	}

	*book = removePrefix(*book, filledPrefix)
	return fills, residual
}

func formatCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}

func (ob *OrderBook) PrintBook(title string) {
	fmt.Printf("\n%s\n", title)
	fmt.Println(strings.Repeat("-", 56))
	fmt.Println("ASKS (low → high):")
	if len(ob.Asks) == 0 {
		fmt.Println("  (empty)")
	}
	for i := len(ob.Asks) - 1; i >= 0; i-- {
		o := ob.Asks[i]
		fmt.Printf("  %s  %s  qty=%d\n", o.ID, formatCents(o.PriceCents), o.Quantity)
	}
	fmt.Println("BIDS (high → low):")
	if len(ob.Bids) == 0 {
		fmt.Println("  (empty)")
	}
	for _, o := range ob.Bids {
		fmt.Printf("  %s  %s  qty=%d\n", o.ID, formatCents(o.PriceCents), o.Quantity)
	}
	fmt.Println(strings.Repeat("-", 56))
}

func main() {
	ob := NewOrderBook()

	ob.AddLimitOrder(Buy, 10000, 5)  // $100.00 x 5
	ob.AddLimitOrder(Buy, 9950, 10)  // $99.50 x 10
	ob.AddLimitOrder(Buy, 9900, 8)   // $99.00 x 8

	ob.AddLimitOrder(Sell, 10050, 4) // $100.50 x 4
	ob.AddLimitOrder(Sell, 10100, 6) // $101.00 x 6
	ob.AddLimitOrder(Sell, 10150, 10) // $101.50 x 10

	ob.PrintBook("Order book BEFORE market buy")

	market := Order{
		ID:       ob.newID("M"),
		Side:     Buy,
		Quantity: 12, // fills 4 + 6 + 2 of the third ask
	}
	fmt.Printf("\nIncoming market %s: qty=%d (crosses multiple asks)\n", market.ID, market.Quantity)

	fills, residual := ob.MatchOrder(market)

	fmt.Println("\nFills:")
	var total int64
	for _, f := range fills {
		total += f.Quantity
		fmt.Printf("  %d @ %s against %s\n", f.Quantity, formatCents(f.PriceCents), f.RestingID)
	}
	fmt.Printf("Total filled: %d | residual unfilled: %d\n", total, residual.Quantity)

	ob.PrintBook("Order book AFTER market buy")
}
