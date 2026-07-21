package engine

import (
	"context"
	"errors"
)

// RateLimitedError signals a 429 from Stoat; Op.Apply implementations
// return it (wrapping RetryAfterSeconds from the response header) so
// runRemote can back off the shared global limiter and retry.
type RateLimitedError struct {
	RetryAfterSeconds float64
}

func (e *RateLimitedError) Error() string {
	return "engine: rate limited by Stoat"
}

// runRemote waits on op's rate-limit bucket, calls Apply, and retries on a
// RateLimitedError up to maxRemoteRetries, backing off that same bucket by
// the server-supplied Retry-After each time.
func (e *Engine) runRemote(op Op) (string, error) {
	ctx := context.Background()
	key := bucketKey(op)

	var lastErr error
	for attempt := 0; attempt <= maxRemoteRetries; attempt++ {
		if err := e.limiter.Wait(ctx, key); err != nil {
			return "", err
		}

		stoatID, err := op.Apply(ctx)
		if err == nil {
			return stoatID, nil
		}

		var rateLimited *RateLimitedError
		if !errors.As(err, &rateLimited) {
			return "", err
		}

		lastErr = err
		e.limiter.Backoff(key, rateLimited.RetryAfterSeconds)
	}

	return "", lastErr
}
