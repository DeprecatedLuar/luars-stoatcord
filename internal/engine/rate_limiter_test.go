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

// TestGlobalRateLimiter_BackoffDuringConcurrentSleepIsHonored reproduces the
// live reconcile cascade: a caller that already committed to an earlier slot
// and is mid-sleep must still honor a Backoff() issued by a different
// concurrent caller's 429 while it sleeps, not fire at its stale pre-backoff
// time. Wait previously froze its sleep duration at call time and never
// re-checked nextAllowed, so a burst of concurrent workers all landed inside
// Stoat's still-active punish window and burned through their retry budget.
func TestGlobalRateLimiter_BackoffDuringConcurrentSleepIsHonored(t *testing.T) {
	limiter := NewGlobalRateLimiter(20 * time.Millisecond)
	ctx := context.Background()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("priming Wait: %v", err)
	}

	start := time.Now()
	done := make(chan struct{})
	go func() {
		limiter.Wait(ctx) // commits to a ~20ms slot before the Backoff below lands
		close(done)
	}()

	time.Sleep(5 * time.Millisecond) // let the goroutine commit its slot and start sleeping
	limiter.Backoff(0.2)             // simulates a concurrent 429 extending the window to 200ms

	<-done
	elapsed := time.Since(start)

	if elapsed < 190*time.Millisecond {
		t.Fatalf("expected the already-sleeping caller to honor the concurrent backoff, elapsed %v", elapsed)
	}
}

func TestBucketKey_RoutesEntityTypesToStoatBuckets(t *testing.T) {
	cases := []struct {
		name string
		op   Op
		want string
	}{
		{"role", Op{EntityType: EntityRole}, "servers"},
		{"category", Op{EntityType: EntityCategory}, "servers"},
		{"server", Op{EntityType: EntityServer}, "servers"},
		{"channel", Op{EntityType: EntityChannel, DiscordID: "chan-1"}, "channels:chan-1"},
		{"message", Op{EntityType: EntityMessage, ChannelID: "chan-2"}, "messaging:chan-2"},
		{"emoji fallback", Op{EntityType: EntityEmoji}, "any"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bucketKey(tc.op); got != tc.want {
				t.Fatalf("bucketKey(%+v) = %q, want %q", tc.op, got, tc.want)
			}
		})
	}
}

func TestBucketedRateLimiter_BackoffIsolatedToItsBucket(t *testing.T) {
	limiter := NewBucketedRateLimiter(map[string]time.Duration{"servers": 0, "messaging": 0}, time.Millisecond)
	ctx := context.Background()

	limiter.Backoff("servers", 10) // 10s backoff on servers only

	start := time.Now()
	if err := limiter.Wait(ctx, "messaging:chan-1"); err != nil {
		t.Fatalf("Wait on messaging bucket: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected messaging bucket to be unaffected by servers backoff, elapsed %v", elapsed)
	}
}

func TestBucketedRateLimiter_PacesWithinSameBucketName(t *testing.T) {
	limiter := NewBucketedRateLimiter(map[string]time.Duration{"servers": 20 * time.Millisecond}, time.Millisecond)
	ctx := context.Background()

	start := time.Now()
	for i := range 6 {
		if err := limiter.Wait(ctx, "servers"); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// 6 calls to the single shared "servers" sub-limiter must space out over
	// at least 5 intervals (100ms), never burst.
	if elapsed < 100*time.Millisecond {
		t.Fatalf("expected servers bucket to pace at >=20ms apart, elapsed %v for 6 calls", elapsed)
	}
}

func TestBucketedRateLimiter_DistinctKeysWithinBucketRunIndependently(t *testing.T) {
	limiter := NewBucketedRateLimiter(map[string]time.Duration{"messaging": 50 * time.Millisecond}, time.Millisecond)
	ctx := context.Background()

	if err := limiter.Wait(ctx, "messaging:chan-a"); err != nil {
		t.Fatalf("prime chan-a: %v", err)
	}

	start := time.Now()
	if err := limiter.Wait(ctx, "messaging:chan-b"); err != nil {
		t.Fatalf("Wait chan-b: %v", err)
	}
	elapsed := time.Since(start)

	// chan-b has its own sub-limiter, so priming chan-a must not delay it.
	if elapsed > 40*time.Millisecond {
		t.Fatalf("expected chan-b to have an independent sub-limiter from chan-a, elapsed %v", elapsed)
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
