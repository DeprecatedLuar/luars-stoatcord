package main

import (
	"testing"
)

func TestCompositeHealthChecker_HealthyWhenBothReady(t *testing.T) {
	c := &compositeHealthChecker{
		discordReady: func() bool { return true },
		stoatHealthy: func() bool { return true },
	}
	ok, degraded := c.Check()
	if !ok || len(degraded) != 0 {
		t.Fatalf("Check() = (%v, %v), want (true, [])", ok, degraded)
	}
}

func TestCompositeHealthChecker_ReportsWhichSideIsDegraded(t *testing.T) {
	c := &compositeHealthChecker{
		discordReady: func() bool { return false },
		stoatHealthy: func() bool { return true },
	}
	ok, degraded := c.Check()
	if ok {
		t.Fatalf("Check() ok = true, want false")
	}
	if len(degraded) != 1 || degraded[0] != "discord" {
		t.Fatalf("degraded = %v, want [discord]", degraded)
	}
}

func TestCompositeHealthChecker_ReportsBothDegraded(t *testing.T) {
	c := &compositeHealthChecker{
		discordReady: func() bool { return false },
		stoatHealthy: func() bool { return false },
	}
	ok, degraded := c.Check()
	if ok {
		t.Fatalf("Check() ok = true, want false")
	}
	if len(degraded) != 2 {
		t.Fatalf("degraded = %v, want 2 entries", degraded)
	}
}
