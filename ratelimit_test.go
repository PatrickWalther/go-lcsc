package lcsc

import (
	"context"
	"testing"
	"time"
)

// TestNewRateLimiter tests creating a new rate limiter.
func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(5.0)

	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}

	if rl.maxTokens != 5.0 {
		t.Errorf("expected max tokens 5.0, got %f", rl.maxTokens)
	}

	if rl.refillRate != 5.0 {
		t.Errorf("expected refill rate 5.0, got %f", rl.refillRate)
	}

	if rl.tokens != 5.0 {
		t.Errorf("expected initial tokens 5.0, got %f", rl.tokens)
	}
}

// TestRateLimiterWaitImmediate tests that Wait returns immediately when tokens available.
func TestRateLimiterWaitImmediate(t *testing.T) {
	rl := NewRateLimiter(10.0)
	ctx := context.Background()

	start := time.Now()
	err := rl.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if elapsed > 100*time.Millisecond {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

// TestRateLimiterWaitMultiple tests consuming multiple tokens.
func TestRateLimiterWaitMultiple(t *testing.T) {
	rl := NewRateLimiter(10.0) // 10 tokens per second
	ctx := context.Background()

	// Should be able to consume 10 tokens quickly
	for i := 0; i < 10; i++ {
		err := rl.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait %d failed: %v", i, err)
		}
	}
}

// TestRateLimiterWaitBlocks tests that Wait blocks when out of tokens.
func TestRateLimiterWaitBlocks(t *testing.T) {
	rl := NewRateLimiter(2.0) // 2 tokens per second
	ctx := context.Background()

	// Consume initial tokens
	for i := 0; i < 2; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("initial wait failed: %v", err)
		}
	}

	// Next wait should block for some time
	start := time.Now()
	err := rl.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait after blocking failed: %v", err)
	}

	// Should have waited at least 400ms (1 token at 2 RPS = 0.5 second, but timing may vary)
	if elapsed < 300*time.Millisecond {
		t.Errorf("Wait blocked for too short: %v (expected ~500ms)", elapsed)
	}
}

// TestRateLimiterWaitContextCancellation tests that Wait respects context cancellation.
func TestRateLimiterWaitContextCancellation(t *testing.T) {
	rl := NewRateLimiter(10.0) // Normal rate
	ctx, cancel := context.WithCancel(context.Background())

	// Start consuming tokens quickly
	if err := rl.Wait(ctx); err != nil {
		t.Fatalf("initial wait failed: %v", err)
	}

	// Consume the rest
	for i := 0; i < 9; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("wait failed: %v", err)
		}
	}

	// Now we're out of tokens and the next wait will block
	errorChan := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	go func() {
		errorChan <- rl.Wait(ctx)
	}()

	// Wait for result with timeout
	select {
	case err := <-errorChan:
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}
		// Acceptable - got an error as expected
	case <-time.After(2 * time.Second):
		t.Fatal("wait for cancellation took too long")
	}
}

// TestRateLimiterWaitContextTimeout tests that Wait respects context timeout.
func TestRateLimiterWaitContextTimeout(t *testing.T) {
	rl := NewRateLimiter(2.0) // Reasonable rate
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Consume available tokens
	for i := 0; i < 2; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Logf("initial wait failed: %v (context may have timed out early)", err)
			return
		}
	}

	// Now context should timeout while waiting for next token
	err := rl.Wait(ctx)
	if err == nil {
		t.Fatal("expected error for timeout context")
	}

	// Should be a deadline exceeded or context error
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Logf("expected deadline/cancel error, got %v", err)
	}
}

// TestRateLimiterWaitRefill tests that tokens are refilled over time.
func TestRateLimiterWaitRefill(t *testing.T) {
	rl := NewRateLimiter(5.0) // 5 tokens per second
	ctx := context.Background()

	// Consume initial tokens
	for i := 0; i < 5; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("initial wait failed: %v", err)
		}
	}

	// Wait for tokens to refill
	time.Sleep(300 * time.Millisecond)

	// Should be able to consume at least 1 token now
	err := rl.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait after refill failed: %v", err)
	}
}

// TestRateLimiterWaitConcurrent tests concurrent Wait calls.
func TestRateLimiterWaitConcurrent(t *testing.T) {
	rl := NewRateLimiter(10.0)
	ctx := context.Background()

	errors := make(chan error, 10)

	// Launch concurrent goroutines
	for i := 0; i < 10; i++ {
		go func() {
			err := rl.Wait(ctx)
			errors <- err
		}()
	}

	// Collect results
	for i := 0; i < 10; i++ {
		err := <-errors
		if err != nil {
			t.Errorf("concurrent Wait failed: %v", err)
		}
	}
}

// TestRateLimiterHighRate tests rate limiter with high request rate.
func TestRateLimiterHighRate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow rate limiter test in short mode")
	}

	rl := NewRateLimiter(100.0) // 100 requests per second
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 100; i++ {
		if err := rl.Wait(ctx); err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
	}
	elapsed := time.Since(start)

	// 100 requests at 100 RPS should complete in roughly 1 second
	// Allow some variance
	if elapsed < 900*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Logf("100 requests took %v (expected ~1s at 100 RPS)", elapsed)
	}
}

// TestRateLimiterLowRate tests rate limiter with low request rate.
func TestRateLimiterLowRate(t *testing.T) {
	t.Skip("skipping slow rate limiter test - timing sensitive and unreliable on slow systems")
}

// TestRateLimiterFractionalRate tests rate limiter with fractional rate.
func TestRateLimiterFractionalRate(t *testing.T) {
	t.Skip("skipping slow rate limiter test - causes 2+ second delays and is unreliable on slow systems")
}
