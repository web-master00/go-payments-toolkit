# Go Payments Toolkit

Three small Go programs for payment handling: charge a key only once, store money as integer cents, and drop filled orders from an exchange book without keeping them alive on the heap.

Each file is its own `package main`. Run them one at a time.

## Programs

| File | What it does |
| --- | --- |
| `idempotent_payment.go` | Remembers a payment by idempotency key so a retry returns the first result instead of charging again. |
| `money_ledger.go` | Stores amounts as `int64` cents and formats them for display. No floating-point money math. |
| `order_book.go` | Removes filled orders from the front of a slice and clears the dropped slots so the garbage collector can reclaim them. |

## Requirements

- Go 1.21 or newer

## Run

```bash
go run idempotent_payment.go
go run money_ledger.go
go run order_book.go
```
