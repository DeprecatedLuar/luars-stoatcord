package stoat

import (
	"context"
	"io"
	"log/slog"
	"testing"
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
