package engine

import "context"

// OpKind selects which mapping-store lifecycle step a resolved event maps
// to (spec: create/update both flow through diff+pending+confirm; delete
// skips diff and removes the mapping row on confirm instead).
type OpKind int

const (
	OpCreate OpKind = iota
	OpUpdate
	OpDelete
)

// DependencyKey names a not-yet-mapped entity another op is waiting on
// (spec 5): e.g. a role-level channel overwrite depends on both the role
// and the channel existing in their mapping tables first.
type DependencyKey struct {
	EntityType EntityType
	DiscordID  string
}

// Op is one resolved event with everything the engine needs to sequence it,
// but nothing about how to compare or reach Stoat -- that lives in Diff and
// Apply, supplied by the caller (internal/discord, internal/reaper,
// internal/reconcile in later phases). This keeps engine a pure sequencer.
type Op struct {
	Kind       OpKind
	EntityType EntityType
	DiscordID  string

	// ChannelID routes message ops to their per-channel serialized worker
	// (send-order = display-order, spec 5). Empty for non-message ops,
	// which take the general dependency-resolver path instead.
	ChannelID string

	// CanonicalState is the desired state to persist into the
	// canonical_state column on write-pending and confirm.
	CanonicalState string

	// DependsOn lists entities that must already be mapped (active) before
	// this op may run. Checked by the engine directly against MappingStore;
	// unmet deps defer the op into the waiters index (spec 5).
	DependsOn []DependencyKey

	// Diff reports whether the desired state (captured by the closure) and
	// the stored canonical_state already agree once translated to Stoat's
	// shape -- a true result skips Apply (idempotent re-run). Nil for
	// OpDelete, and for a first-time create where nothing is stored yet.
	Diff func(storedCanonicalState string) (equal bool, err error)

	// Apply performs the actual remote write and returns the Stoat id to
	// confirm (ignored for OpDelete).
	Apply func(ctx context.Context) (stoatID string, err error)
}
