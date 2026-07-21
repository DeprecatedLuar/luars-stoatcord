package engine

import (
	"context"
	"strings"
	"sync"
	"time"
)

// GlobalRateLimiter is a single min-interval throttle. It is the primitive
// BucketedRateLimiter wraps -- one instance per Stoat rate-limit bucket, not
// a global throttle shared by every op. minInterval is the floor spacing
// between remote calls on this bucket, source-ground-truthed from Stoat's
// own rate limiter as 10s/limit for the bucket (see bucketKey).
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

// BucketedRateLimiter dispatches each op to its own independent
// GlobalRateLimiter, keyed by bucketKey, so structure ops (the shared
// "servers" bucket) self-pace to Stoat's real per-bucket limit without
// throttling unrelated message/channel ops on their own buckets. A bucket
// name is the key up to (not including) its first ":" -- "channels:<id>"
// and "messaging:<id>" share one interval per name but get one sub-limiter
// per distinct key (independent pacing per channel).
type BucketedRateLimiter struct {
	mu              sync.Mutex
	limiters        map[string]*GlobalRateLimiter
	intervals       map[string]time.Duration
	defaultInterval time.Duration
}

func NewBucketedRateLimiter(intervals map[string]time.Duration, defaultInterval time.Duration) *BucketedRateLimiter {
	return &BucketedRateLimiter{
		limiters:        make(map[string]*GlobalRateLimiter),
		intervals:       intervals,
		defaultInterval: defaultInterval,
	}
}

func (b *BucketedRateLimiter) subLimiter(key string) *GlobalRateLimiter {
	b.mu.Lock()
	defer b.mu.Unlock()

	if l, ok := b.limiters[key]; ok {
		return l
	}

	name, _, _ := strings.Cut(key, ":")
	interval, ok := b.intervals[name]
	if !ok {
		interval = b.defaultInterval
	}

	l := NewGlobalRateLimiter(interval)
	b.limiters[key] = l
	return l
}

func (b *BucketedRateLimiter) Wait(ctx context.Context, bucketKey string) error {
	return b.subLimiter(bucketKey).Wait(ctx)
}

func (b *BucketedRateLimiter) Backoff(bucketKey string, retryAfterSeconds float64) {
	b.subLimiter(bucketKey).Backoff(retryAfterSeconds)
}

// bucketKey derives the Stoat rate-limit bucket an op's remote call falls
// into (crates/core/ratelimits/src/ratelimiter.rs, crates/delta/src/util/
// ratelimits.rs): role/category/server metadata writes all hit the single
// shared "servers" bucket, channel structure writes hit "channels" keyed per
// channel, message sends hit "messaging" keyed per channel, and anything
// else falls back to Stoat's default "any" bucket.
func bucketKey(op Op) string {
	switch op.EntityType {
	case EntityRole, EntityCategory, EntityServer:
		return "servers"
	case EntityChannel:
		return "channels:" + op.DiscordID
	case EntityMessage:
		return "messaging:" + op.ChannelID
	default:
		return "any"
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
