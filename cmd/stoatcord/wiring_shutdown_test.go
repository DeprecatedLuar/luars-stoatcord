package main

import (
	"testing"
	"time"
)

func TestDrainEngine_ReturnsFalseWhenWaitFinishesBeforeTimeout(t *testing.T) {
	wait := func() {}

	if timedOut := drainEngine(wait, 100*time.Millisecond); timedOut {
		t.Fatal("drainEngine reported timedOut=true for a wait() that returns immediately")
	}
}

// A permanently blocked wait (e.g. eng.Wait() on an op deferred forever on
// an unmet dependency, see internal/engine) must not wedge shutdown.
func TestDrainEngine_ReturnsTrueWhenWaitNeverReturns(t *testing.T) {
	block := make(chan struct{})
	wait := func() { <-block }

	start := time.Now()
	timedOut := drainEngine(wait, 50*time.Millisecond)
	elapsed := time.Since(start)

	if !timedOut {
		t.Fatal("drainEngine reported timedOut=false for a wait() that never returns")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("drainEngine took %v to give up, want close to the 50ms timeout", elapsed)
	}
}
