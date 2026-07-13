/*
# FinTech Fixed-Point Money Ledger

## What it is

The fixed-point integer math pattern for financial representation:

- **Lowest denomination** - Amounts are stored as `int64` cents (or the
  currency's smallest unit), not as `float64` dollars. `$10.50` becomes `1050`.
- **Exact arithmetic** - `Add` and `Subtract` operate on whole integers, so
  results never accumulate binary floating-point rounding error.
- **Display formatting** - A dedicated formatter splits cents into dollars and
  a two-digit fractional part only when presenting values to humans or APIs.

## What it is used for

Preventing catastrophic fractional penny loss in banking software,
cryptocurrency exchanges, and payment gateways:

- **Ledgers & balances** - Keep account books reconcilable to the cent across
  millions of deposits and withdrawals.
- **Settlement & clearing** - Move value between parties without drift that
  would otherwise appear after repeated float math.
- **Fee and FX pipelines** - Apply integer-safe rules (truncate, round half-up)
  explicitly instead of inheriting IEEE-754 surprises.
*/

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money stores a currency amount in the lowest denomination (cents).
type Money struct {
	Cents int64
}

func NewMoney(cents int64) Money {
	return Money{Cents: cents}
}

// ParseMoney converts a decimal dollar string (e.g. "10.50", "0.01") into cents.
// No floating-point types are used.
func ParseMoney(dollars string) (Money, error) {
	dollars = strings.TrimSpace(dollars)
	if dollars == "" {
		return Money{}, errors.New("empty amount")
	}

	negative := false
	if strings.HasPrefix(dollars, "-") {
		negative = true
		dollars = dollars[1:]
	} else if strings.HasPrefix(dollars, "+") {
		dollars = dollars[1:]
	}
	dollars = strings.TrimPrefix(dollars, "$")

	parts := strings.Split(dollars, ".")
	if len(parts) > 2 {
		return Money{}, fmt.Errorf("invalid amount: %q", dollars)
	}

	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("invalid dollars: %w", err)
	}

	var frac int64
	if len(parts) == 2 {
		fracStr := parts[1]
		if len(fracStr) == 0 || len(fracStr) > 2 {
			return Money{}, fmt.Errorf("cents must be 1–2 digits: %q", fracStr)
		}
		if len(fracStr) == 1 {
			fracStr += "0"
		}
		frac, err = strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return Money{}, fmt.Errorf("invalid cents: %w", err)
		}
	}

	cents := whole*100 + frac
	if negative {
		cents = -cents
	}
	return Money{Cents: cents}, nil
}

func (m Money) Add(other Money) Money {
	return Money{Cents: m.Cents + other.Cents}
}

func (m Money) Subtract(other Money) (Money, error) {
	if other.Cents > m.Cents {
		return Money{}, fmt.Errorf(
			"insufficient funds: have %s, need %s",
			m.Display(), other.Display(),
		)
	}
	return Money{Cents: m.Cents - other.Cents}, nil
}

// Display formats the amount as a standard decimal currency string (e.g. $10.50).
func (m Money) Display() string {
	sign := ""
	cents := m.Cents
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	dollars := cents / 100
	frac := cents % 100
	return fmt.Sprintf("%s$%d.%02d", sign, dollars, frac)
}

func mustParse(s string) Money {
	m, err := ParseMoney(s)
	if err != nil {
		panic(err)
	}
	return m
}

func main() {
	balance := NewMoney(0)
	expected := int64(0)

	fmt.Println("FinTech ledger (int64 cents - no float64)")
	fmt.Println(strings.Repeat("=", 48))
	fmt.Printf("Opening balance: %s\n\n", balance.Display())

	type op struct {
		kind   string
		amount string
	}
	ops := []op{
		{"deposit", "10.10"},
		{"deposit", "0.01"},
		{"deposit", "3.33"},
		{"withdraw", "1.15"},
		{"deposit", "0.25"},
		{"withdraw", "0.99"},
		{"deposit", "2.50"},
	}

	for _, o := range ops {
		amt := mustParse(o.amount)
		switch o.kind {
		case "deposit":
			balance = balance.Add(amt)
			expected += amt.Cents
			fmt.Printf("Deposit  %7s  → balance %s\n", amt.Display(), balance.Display())
		case "withdraw":
			next, err := balance.Subtract(amt)
			if err != nil {
				fmt.Printf("Withdraw %7s  → FAILED: %v\n", amt.Display(), err)
				continue
			}
			balance = next
			expected -= amt.Cents
			fmt.Printf("Withdraw %7s  → balance %s\n", amt.Display(), balance.Display())
		}
	}

	fmt.Println(strings.Repeat("=", 48))
	fmt.Printf("Final balance:  %s (%d cents)\n", balance.Display(), balance.Cents)
	fmt.Printf("Expected total: %s (%d cents)\n", NewMoney(expected).Display(), expected)

	if balance.Cents == expected {
		fmt.Println("Ledger balanced: exact integer reconciliation, zero drift.")
	} else {
		fmt.Printf("Ledger MISMATCH: got %d cents, want %d\n", balance.Cents, expected)
	}
}
