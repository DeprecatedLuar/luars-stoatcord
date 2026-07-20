package engine

import (
	"context"
	"sync"
	"time"
)

// GlobalRateLimiter is the single throttle shared by every worker (the
// Stoat API limit is global, not per-channel). minInterval is the floor
// spacing between remote calls; it is not a spec-known constant (Phase 0
// didn't observe Stoat's exact rate limit), so it is a constructor
// parameter the caller supplies from real wiring, not a hardcoded value.
type GlobalRateLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	nextAllowed time.Time
}

func NewGlobalRateLimiter(minInterval time.Duration) *GlobalRateLimiter {
	return &GlobalRateLimiter{minInterval: minInterval}
}

// Wait blocks until the shared limiter allows the next remote call, honoring
// both the minimum-interval floor and any active Backoff window, or returns
// ctx.Err() if the context is done first.
//
// It re-checks nextAllowed after every sleep instead of committing to a
// duration computed once at call time: a concurrent caller's Backoff (from
// its own 429) can land while this caller is already asleep on an
// earlier-reserved slot, and that extended window must still be honored, or
// a burst of concurrent callers all fire back into Stoat's still-active
// punish window regardless of the backoff.
func (l *GlobalRateLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		wait := l.nextAllowed.Sub(now)
		if wait <= 0 {
			next := now
			if l.nextAllowed.After(next) {
				next = l.nextAllowed
			}
			l.nextAllowed = next.Add(l.minInterval)
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

// Backoff extends the shared limiter's next-allowed time by a Stoat 429
// Retry-After response, so every subsequent caller (not just the one that
// hit the 429) waits it out.
func (l *GlobalRateLimiter) Backoff(retryAfterSeconds float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	until := time.Now().Add(time.Duration(retryAfterSeconds * float64(time.Second)))
	if until.After(l.nextAllowed) {
		l.nextAllowed = until
	}
}
