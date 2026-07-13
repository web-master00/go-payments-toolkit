/*
# Idempotent Payment Processor

## What it is

Idempotency and concurrent-safe shared state for payment handling:

- **Idempotency** - Performing the same logical operation multiple times with the
  same key produces one side effect and the same success result. Retries are
  safe: duplicate submissions return the cached outcome instead of charging again.
- **`sync.RWMutex`** - Guards the in-memory map of processed transaction IDs.
  Many goroutines can `RLock` to read concurrently; a writer takes an exclusive
  `Lock` to insert a newly processed payment. A double-checked lock pattern
  closes the race between “key missing” under a read lock and “insert” under
  a write lock.

## What it is used for

Ensuring that if a user's network drops and they click Pay twice, they are only
charged once:

- **Client retries** - Mobile apps and browsers resend the same idempotency key
  after timeouts; the gateway recognizes the key and returns the prior result.
- **At-least-once delivery** - Message queues and load balancers may deliver
  duplicates; server-side idempotency makes those duplicates harmless.
- **Ledger integrity** - Prevents double-settlement without requiring the user
  to understand distributed-systems failure modes.
*/

package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	statusProcessed = "processed"
	statusCached    = "cached"
	statusRejected  = "rejected"
)

// PaymentResult is the outcome returned for a ProcessPayment call.
type PaymentResult struct {
	IdempotencyKey string
	AmountCents    int64
	Status         string
	Message        string
}

// PaymentProcessor stores processed transaction IDs behind an RWMutex.
type PaymentProcessor struct {
	mu           sync.RWMutex
	processed    map[string]PaymentResult
	processCount int64
}

func NewPaymentProcessor() *PaymentProcessor {
	return &PaymentProcessor{
		processed: make(map[string]PaymentResult),
	}
}

// ProcessPayment charges at most once per idempotency key.
func (p *PaymentProcessor) ProcessPayment(key string, amountCents int64) PaymentResult {
	if key == "" {
		return PaymentResult{
			IdempotencyKey: key,
			AmountCents:    amountCents,
			Status:         statusRejected,
			Message:        "idempotency key is required",
		}
	}

	// Fast path: concurrent readers.
	p.mu.RLock()
	if cached, ok := p.processed[key]; ok {
		p.mu.RUnlock()
		cached.Status = statusCached
		cached.Message = "returning cached payment result"
		return cached
	}
	p.mu.RUnlock()

	// Slow path: exclusive writer with double-check.
	p.mu.Lock()
	defer p.mu.Unlock()

	if cached, ok := p.processed[key]; ok {
		cached.Status = statusCached
		cached.Message = "returning cached payment result"
		return cached
	}

	// Simulate gateway charge latency.
	time.Sleep(25 * time.Millisecond)

	p.processCount++
	result := PaymentResult{
		IdempotencyKey: key,
		AmountCents:    amountCents,
		Status:         statusProcessed,
		Message:        "payment processed successfully",
	}
	p.processed[key] = result
	return result
}

func (p *PaymentProcessor) ProcessCount() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.processCount
}

func fireBurst(p *PaymentProcessor, key string, amountCents int64, n int) []PaymentResult {
	results := make(chan PaymentResult, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- p.ProcessPayment(key, amountCents)
		}()
	}

	wg.Wait()
	close(results)

	out := make([]PaymentResult, 0, n)
	for r := range results {
		out = append(out, r)
	}
	return out
}

func summarize(label string, results []PaymentResult, processCount int64) {
	processed, cached, rejected := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case statusProcessed:
			processed++
		case statusCached: 
			cached++
		case statusRejected:
			rejected++
		}
	}

	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("requests: %d | processed: %d | cached: %d | rejected: %d\n",
		len(results), processed, cached, rejected)
	fmt.Printf("processor processCount: %d\n", processCount)

	if len(results) > 0 {
		sample := results[0]
		fmt.Printf("sample: key=%s amount=%d¢ status=%s - %s\n",
			sample.IdempotencyKey, sample.AmountCents, sample.Status, sample.Message)
	}
}

func main() {
	p := NewPaymentProcessor()

	const keyA = "pay-order-42"
	const amountA int64 = 2599

	burstA := fireBurst(p, keyA, amountA, 20)
	summarize("20 identical requests (pay-order-42)", burstA, p.ProcessCount())

	processedA, cachedA := 0, 0
	for _, r := range burstA {
		if r.Status == statusProcessed {
			processedA++
		}
		if r.Status == statusCached {
			cachedA++
		}
	}
	if processedA == 1 && cachedA == 19 && p.ProcessCount() == 1 {
		fmt.Println("PROOF: only one charge ran; 19 callers got the cached result.")
	} else {
		fmt.Printf("UNEXPECTED: processed=%d cached=%d processCount=%d\n",
			processedA, cachedA, p.ProcessCount())
	}

	const keyB = "pay-order-99"
	const amountB int64 = 1000
	burstB := fireBurst(p, keyB, amountB, 5)
	summarize("5 identical requests (pay-order-99)", burstB, p.ProcessCount())

	if p.ProcessCount() == 2 {
		fmt.Println("PROOF: second key charged exactly once (processCount=2 total).")
	} else {
		fmt.Printf("UNEXPECTED total processCount=%d (want 2)\n", p.ProcessCount())
	}
}
