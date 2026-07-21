// Package stoat wraps within.website/x/web/revolt (implementation-plan.md
// "Stoat/Revolt library notes"), confined here per the architecture
// guardrail: the community library is imported only inside this package.
package stoat

import (
	"time"

	"within.website/x/web/revolt"
)

// libraryTickerInterval replaces the library's own hardcoded 3-second
// Ticker (gap 4): every Client.Request/RequestWithPathAndContentType call
// blocks on <-c.Ticker.C, an undocumented throttle baked into the library
// itself. Our architecture's single intended throttle is the caller-side
// engine.GlobalRateLimiter (spec: one global throttle shared by every
// worker), so this internal one is neutralized to effectively no-op rather
// than left to double-gate -- and far more slowly -- every remote call.
const libraryTickerInterval = time.Nanosecond

// Client wraps *revolt.Client, working around known library gaps.
type Client struct {
	inner *revolt.Client
	// elevation caches the bot's own elevation role, resolved once at
	// startup by ResolveElevationRole. Set before any concurrent op
	// traffic begins, so a plain field (no mutex) is safe.
	elevation *elevationRole
}

// New constructs a Client against apiBase and immediately overrides
// WSURL from the fetched Settings.Ws (gap 1: revolt.New/NewWithEndpoint
// hardcode WSURL to the official wss://ws.revolt.chat and never read it
// from the instance's own advertised gateway).
func New(token, apiBase string) (*Client, error) {
	inner, err := revolt.NewWithEndpoint(token, apiBase, "")
	if err != nil {
		return nil, err
	}
	inner.WSURL = inner.Settings.Ws
	inner.Ticker.Stop()
	inner.Ticker = time.NewTicker(libraryTickerInterval)
	return &Client{inner: inner}, nil
}

// WSURL exposes the corrected gateway URL for tests/diagnostics.
func (c *Client) WSURL() string {
	return c.inner.WSURL
}
