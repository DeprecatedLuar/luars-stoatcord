package engine

import (
	"context"
	"testing"
	"time"
)

func TestGlobalRateLimiter_EnforcesMinimumInterval(t *testing.T) {
	limiter := NewGlobalRateLimiter(30 * time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("second Wait: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 30*time.Millisecond {
		t.Fatalf("expected second Wait to block for at least the minimum interval, elapsed %v", elapsed)
	}
}

func TestGlobalRateLimiter_BackoffDelaysNextWait(t *testing.T) {
	limiter := NewGlobalRateLimiter(0)
	ctx := context.Background()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	limiter.Backoff(0.05) // 50ms, as Stoat's Retry-After would report

	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("second Wait: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 45*time.Millisecond {
		t.Fatalf("expected Wait to honor the 429 Retry-After backoff, elapsed %v", elapsed)
	}
}

func TestGlobalRateLimiter_Wait_RespectsContextCancellation(t *testing.T) {
	limiter := NewGlobalRateLimiter(0)
	limiter.Backoff(10) // 10s backoff, far longer than the context timeout below

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err == nil {
		t.Fatal("expected Wait to return the context error instead of blocking for the full backoff")
	}
}

func TestGlobalRateLimiter_SharedAcrossConcurrentCallers(t *testing.T) {
	limiter := NewGlobalRateLimiter(20 * time.Millisecond)
	ctx := context.Background()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("priming Wait: %v", err)
	}

	start := time.Now()
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			limiter.Wait(ctx)
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	elapsed := time.Since(start)

	// Two more callers sharing one global limiter must serialize through it:
	// at least two more minimum-interval slots elapse, not one.
	if elapsed < 40*time.Millisecond {
		t.Fatalf("expected concurrent callers to serialize through the shared limiter, elapsed %v", elapsed)
	}
}
