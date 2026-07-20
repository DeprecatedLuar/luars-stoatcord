// Package engine is the orchestrator (spec: architecture guardrails): it
// sequences health-check -> diff -> write-pending -> remote -> confirm for
// every op and holds zero translation logic of its own. Diff/Apply are
// closures supplied by the caller (internal/discord, internal/reaper,
// internal/reconcile in later phases) so engine never needs to know how a
// given entity type compares or writes to Stoat.
package engine

import "context"

// EntityType identifies which mapping table an op's identity belongs to.
type EntityType string

const (
	EntityServer   EntityType = "server"
	EntityCategory EntityType = "category"
	EntityChannel  EntityType = "channel"
	EntityRole     EntityType = "role"
	EntityEmoji    EntityType = "emoji"
	EntityMessage  EntityType = "message"
)

// MappingStatus mirrors the status column shared by every mapping table
// (spec 3): pending during the write-intent window, active once confirmed.
type MappingStatus string

const (
	StatusPending  MappingStatus = "pending"
	StatusActive   MappingStatus = "active"
	StatusDeleting MappingStatus = "deleting"
)

// Mapping is a read of one entity's row in its mapping table.
type Mapping struct {
	Found          bool
	StoatID        string
	Status         MappingStatus
	CanonicalState string
}

// HasStoatEntity reports whether m already identifies a real, existing
// Stoat entity -- the single source of truth for every create-vs-edit
// decision in this codebase (internal/discord's applyCreateOrEdit,
// Engine.logDryRun). Deliberately keys off StoatID, not Status: process()
// always calls WritePending -- which flips Status to pending but preserves
// any existing StoatID -- immediately before Apply runs, so Status reads
// pending on every live update to an already-bound entity regardless of
// whether it's genuinely new. StoatID is empty only for a true first-time
// create (store.WritePending's first-insert case); once WritePending
// preserves a real id across an update's pending window, that id is the
// only reliable signal an entity already exists remotely.
func (m Mapping) HasStoatEntity() bool {
	return m.Found && m.StoatID != ""
}

// MappingStore is the generic persistence interface over the five
// common-shape mapping tables (server/category/channel/role/emoji, spec 3).
// It is identity/persistence only, never translation -- what to compare or
// how to reach Stoat lives in the caller-supplied Op.Diff/Op.Apply.
//
// entityType is a plain string (the EntityType's underlying value), not the
// EntityType type itself, so internal/store's implementation never needs to
// import internal/engine to satisfy this interface -- the dependency stays
// one-directional (engine depends on store's concrete type, not the other
// way around).
type MappingStore interface {
	Get(entityType, discordID string) (Mapping, error)
	WritePending(entityType, discordID, canonicalState string) error
	Confirm(entityType, discordID, stoatID string) error
	Remove(entityType, discordID string) error
}

// HealthChecker reports cached connection health for the per-op write gate
// (spec: not a heartbeat subsystem, does not itself recover missed events).
// cmd/stoatcord composes the Discord and Stoat adapters' own health into one
// HealthChecker so engine stays ignorant of how many gateways exist.
type HealthChecker interface {
	Check() (ok bool, degraded []string)
}

// QueueStore persists an op that couldn't be applied while unhealthy, for
// later durable-queue drain (Phase 7). Engine only calls Enqueue; draining
// is reconcile's job.
type QueueStore interface {
	Enqueue(opType, payload string) error
}

// RateLimiter is the single global write throttle shared by every worker
// (the Stoat API limit is global, not per-channel).
type RateLimiter interface {
	Wait(ctx context.Context) error
	Backoff(retryAfterSeconds float64)
}
