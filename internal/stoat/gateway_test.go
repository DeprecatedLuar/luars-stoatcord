package stoat

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGateway_HealthyFalseUntilReady(t *testing.T) {
	g := NewGateway(discardLogger())
	if g.Healthy() {
		t.Fatalf("Healthy() = true before Ready, want false")
	}
}

func TestGateway_HealthyTrueAfterReady(t *testing.T) {
	g := NewGateway(discardLogger())
	if err := g.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !g.Healthy() {
		t.Fatalf("Healthy() = false after Ready, want true")
	}
}

func TestGateway_HealthyFalseAfterUnknownEventError(t *testing.T) {
	g := NewGateway(discardLogger())
	if err := g.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	g.MarkDisconnected()
	if g.Healthy() {
		t.Fatalf("Healthy() = true after MarkDisconnected, want false")
	}
}

func TestWaitForHealthy_TimesOutWhenNeverHealthy(t *testing.T) {
	gw := NewGateway(discardLogger())

	start := time.Now()
	err := WaitForHealthy(context.Background(), gw, 50*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WaitForHealthy returned nil, want a timeout error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WaitForHealthy took %v to give up, want close to the 50ms timeout", elapsed)
	}
}
