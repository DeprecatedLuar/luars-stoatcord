package main

import (
	"time"

	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/store"
)

// drainEngine waits for wait (normally eng.Wait) to return, or gives up
// after timeout and reports timedOut=true. An op permanently deferred on a
// dependency that never confirms (a stuck remote failure, a stale pending
// mapping row) would otherwise block eng.Wait() forever and wedge shutdown
// on SIGTERM/SIGINT.
func drainEngine(wait func(), timeout time.Duration) (timedOut bool) {
	drained := make(chan struct{})
	go func() {
		wait()
		close(drained)
	}()

	select {
	case <-drained:
		return false
	case <-time.After(timeout):
		return true
	}
}

// mappingStoreAdapter satisfies engine.MappingStore over *store.Store.
// store.Mapping and engine.Mapping are structurally identical but distinct
// named types (internal/store must not import internal/engine), so Get is
// the only method that needs translating; WritePending/Confirm/Remove/
// Enqueue already match their interface signatures via embedding.
type mappingStoreAdapter struct {
	*store.Store
}

func (a mappingStoreAdapter) Get(entityType, discordID string) (engine.Mapping, error) {
	m, err := a.Store.Get(entityType, discordID)
	if err != nil {
		return engine.Mapping{}, err
	}
	return engine.Mapping{
		Found:          m.Found,
		StoatID:        m.StoatID,
		Status:         engine.MappingStatus(m.Status),
		CanonicalState: m.CanonicalState,
	}, nil
}
