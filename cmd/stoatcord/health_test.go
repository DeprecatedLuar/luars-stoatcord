package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/stoat"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCompositeHealthChecker_HealthyWhenBothReady(t *testing.T) {
	session := &discordgo.Session{DataReady: true}
	gw := stoat.NewGateway(discardLogger())
	gw.Ready(nil)

	c := &compositeHealthChecker{discordSession: session, stoatGateway: gw}
	ok, degraded := c.Check()
	if !ok || len(degraded) != 0 {
		t.Fatalf("Check() = (%v, %v), want (true, [])", ok, degraded)
	}
}

func TestCompositeHealthChecker_ReportsWhichSideIsDegraded(t *testing.T) {
	session := &discordgo.Session{DataReady: false}
	gw := stoat.NewGateway(discardLogger())
	gw.Ready(nil)

	c := &compositeHealthChecker{discordSession: session, stoatGateway: gw}
	ok, degraded := c.Check()
	if ok {
		t.Fatalf("Check() ok = true, want false")
	}
	if len(degraded) != 1 || degraded[0] != "discord" {
		t.Fatalf("degraded = %v, want [discord]", degraded)
	}
}

func TestCompositeHealthChecker_ReportsBothDegraded(t *testing.T) {
	session := &discordgo.Session{DataReady: false}
	gw := stoat.NewGateway(discardLogger())

	c := &compositeHealthChecker{discordSession: session, stoatGateway: gw}
	ok, degraded := c.Check()
	if ok {
		t.Fatalf("Check() ok = true, want false")
	}
	if len(degraded) != 2 {
		t.Fatalf("degraded = %v, want 2 entries", degraded)
	}
}
