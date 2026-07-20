package stoat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"within.website/x/web/revolt"
)

// stoatHealthyPollInterval bounds how often WaitForHealthy re-checks
// Healthy() while waiting for the Bonfire connection to come up.
const stoatHealthyPollInterval = 100 * time.Millisecond

// Gateway tracks Bonfire (Stoat gateway) connection health for the engine's
// HealthChecker (cmd/stoatcord composes this with Discord's own health).
// It embeds revolt.NullHandler so it satisfies revolt.Handler with only
// Ready/Authenticated/UnknownEvent overridden here; Phase 6's reaper
// replaces this with a full handler that also forwards canonicalized
// events, per the architecture guardrail that internal/stoat owns the
// Bonfire connection.
type Gateway struct {
	revolt.NullHandler

	log *slog.Logger

	mu      sync.Mutex
	healthy bool
}

// NewGateway constructs a Gateway, unhealthy until Ready fires.
func NewGateway(log *slog.Logger) *Gateway {
	return &Gateway{log: log}
}

// Authenticated logs the first stage of the Bonfire handshake (token
// accepted, Ready not yet sent) -- observability for "is the bot actually
// connecting" without waiting on Ready.
func (g *Gateway) Authenticated(ctx context.Context) error {
	g.log.Info("stoat: bonfire authenticated")
	return nil
}

// Ready marks the gateway healthy; satisfies revolt.Handler.
func (g *Gateway) Ready(ctx context.Context) error {
	g.log.Info("stoat: bonfire ready")
	g.mu.Lock()
	g.healthy = true
	g.mu.Unlock()
	return nil
}

// UnknownEvent logs any Bonfire event type the library doesn't decode, so a
// protocol mismatch is visible instead of silently dropped.
func (g *Gateway) UnknownEvent(ctx context.Context, kind string, data []byte) error {
	g.log.Warn("stoat: unknown bonfire event", "kind", kind)
	return nil
}

// MarkDisconnected marks the gateway unhealthy, for the reconnect-loop
// caller to report a dropped connection between Ready events.
func (g *Gateway) MarkDisconnected() {
	g.mu.Lock()
	g.healthy = false
	g.mu.Unlock()
}

// Healthy reports the last known Bonfire connection state.
func (g *Gateway) Healthy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.healthy
}

// Connect starts the Bonfire connection in the background (the library's
// own Connect is non-blocking and self-reconnects).
func (c *Client) Connect(ctx context.Context, handler revolt.Handler) {
	c.inner.Connect(ctx, handler)
}

// WaitForHealthy polls gw until it reports healthy, or ctx/timeout expires
// first.
func WaitForHealthy(ctx context.Context, gw *Gateway, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if gw.Healthy() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for stoat gateway to become healthy", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stoatHealthyPollInterval):
		}
	}
}
