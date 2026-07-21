package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeMappingStore is an in-memory MappingStore for engine unit tests.
// Engine calls it from multiple goroutines (per-channel workers, deferred
// retries), so it needs the same concurrency safety a real store would get
// for free from sql.DB's connection pool.
type fakeMappingStore struct {
	mu   sync.Mutex
	rows map[string]map[string]Mapping
}

func newFakeMappingStore() *fakeMappingStore {
	return &fakeMappingStore{rows: map[string]map[string]Mapping{}}
}

func (f *fakeMappingStore) Get(entityType, discordID string) (Mapping, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if byID, ok := f.rows[entityType]; ok {
		if m, ok := byID[discordID]; ok {
			return m, nil
		}
	}
	return Mapping{}, nil
}

func (f *fakeMappingStore) WritePending(entityType, discordID, canonicalState string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	byID, ok := f.rows[entityType]
	if !ok {
		byID = map[string]Mapping{}
		f.rows[entityType] = byID
	}
	existing := byID[discordID]
	existing.Found = true
	existing.Status = StatusPending
	existing.CanonicalState = canonicalState
	byID[discordID] = existing
	return nil
}

func (f *fakeMappingStore) Confirm(entityType, discordID, stoatID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	byID, ok := f.rows[entityType]
	if !ok {
		byID = map[string]Mapping{}
		f.rows[entityType] = byID
	}
	m := byID[discordID]
	m.Found = true
	m.Status = StatusActive
	m.StoatID = stoatID
	byID[discordID] = m
	return nil
}

func (f *fakeMappingStore) Remove(entityType, discordID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows[entityType], discordID)
	return nil
}

// fakeHealthChecker reports a fixed health state.
type fakeHealthChecker struct {
	ok       bool
	degraded []string
}

func (f *fakeHealthChecker) Check() (bool, []string) { return f.ok, f.degraded }

// fakeQueueStore records enqueued ops for degraded-window assertions.
type fakeQueueStore struct {
	enqueued []string
}

func (f *fakeQueueStore) Enqueue(opType, payload string) error {
	f.enqueued = append(f.enqueued, opType)
	return nil
}

// fakeRateLimiter never blocks or backs off; used where rate limiting isn't
// under test.
type fakeRateLimiter struct{}

func (fakeRateLimiter) Wait(ctx context.Context, bucketKey string) error    { return nil }
func (fakeRateLimiter) Backoff(bucketKey string, retryAfterSeconds float64) {}

func newTestEngine(mappings MappingStore, health HealthChecker, queue QueueStore) *Engine {
	return New(mappings, health, queue, fakeRateLimiter{}, nil)
}

func TestSubmit_CreateOp_WritesPendingThenConfirms(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	applied := false
	op := Op{
		Kind:           OpCreate,
		EntityType:     EntityChannel,
		DiscordID:      "chan-1",
		CanonicalState: `{"name":"general"}`,
		Apply: func(ctx context.Context) (string, error) {
			applied = true
			return "stoat-chan-1", nil
		},
	}

	e.Submit(op)
	e.Wait()

	if !applied {
		t.Fatal("expected Apply to be called for a create op")
	}
	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.Status != StatusActive || m.StoatID != "stoat-chan-1" {
		t.Fatalf("expected confirmed active mapping with stoat id, got %+v", m)
	}
}

func TestSubmit_UpdateOp_SkipsApplyWhenDiffReportsEqual(t *testing.T) {
	mappings := newFakeMappingStore()
	mappings.rows[string(EntityChannel)] = map[string]Mapping{
		"chan-1": {
			Found:          true,
			Status:         StatusActive,
			StoatID:        "stoat-chan-1",
			CanonicalState: `{"name":"general"}`,
		},
	}
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	applyCalled := false
	op := Op{
		Kind:           OpUpdate,
		EntityType:     EntityChannel,
		DiscordID:      "chan-1",
		CanonicalState: `{"name":"general"}`,
		Diff: func(storedCanonicalState string) (bool, error) {
			return storedCanonicalState == `{"name":"general"}`, nil
		},
		Apply: func(ctx context.Context) (string, error) {
			applyCalled = true
			return "stoat-chan-1", nil
		},
	}

	e.Submit(op)
	e.Wait()

	if applyCalled {
		t.Fatal("expected Apply to be skipped when Diff reports already equal (idempotent re-run)")
	}
}

func TestSubmit_UpdateOp_AppliesWhenDiffReportsChanged(t *testing.T) {
	mappings := newFakeMappingStore()
	mappings.rows[string(EntityChannel)] = map[string]Mapping{
		"chan-1": {
			Found:          true,
			Status:         StatusActive,
			StoatID:        "stoat-chan-1",
			CanonicalState: `{"name":"old"}`,
		},
	}
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	applyCalled := false
	op := Op{
		Kind:           OpUpdate,
		EntityType:     EntityChannel,
		DiscordID:      "chan-1",
		CanonicalState: `{"name":"new"}`,
		Diff: func(storedCanonicalState string) (bool, error) {
			return storedCanonicalState == `{"name":"new"}`, nil
		},
		Apply: func(ctx context.Context) (string, error) {
			applyCalled = true
			return "stoat-chan-1", nil
		},
	}

	e.Submit(op)
	e.Wait()

	if !applyCalled {
		t.Fatal("expected Apply to run when Diff reports a change")
	}
	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.CanonicalState != `{"name":"new"}` {
		t.Fatalf("expected stored canonical_state updated to new desired state, got %q", m.CanonicalState)
	}
}

func TestSubmit_DeleteOp_RemovesMapping(t *testing.T) {
	mappings := newFakeMappingStore()
	mappings.rows[string(EntityChannel)] = map[string]Mapping{
		"chan-1": {Found: true, Status: StatusActive, StoatID: "stoat-chan-1"},
	}
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	op := Op{
		Kind:       OpDelete,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply: func(ctx context.Context) (string, error) {
			return "", nil
		},
	}

	e.Submit(op)
	e.Wait()

	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.Found {
		t.Fatalf("expected mapping removed after delete op confirms, got %+v", m)
	}
}

func TestSubmit_UnhealthyGate_EnqueuesInsteadOfApplying(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: false, degraded: []string{"stoat"}}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	applyCalled := false
	op := Op{
		Kind:           OpCreate,
		EntityType:     EntityChannel,
		DiscordID:      "chan-1",
		CanonicalState: `{"name":"general"}`,
		Apply: func(ctx context.Context) (string, error) {
			applyCalled = true
			return "stoat-chan-1", nil
		},
	}

	e.Submit(op)
	e.Wait()

	if applyCalled {
		t.Fatal("expected Apply not to run while unhealthy")
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("expected op enqueued to durable queue while unhealthy, got %d enqueued", len(queue.enqueued))
	}
}

func TestSubmit_DeferredOp_RunsAfterDependencyConfirms(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	dependentApplied := false
	dependent := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		DependsOn:  []DependencyKey{{EntityType: EntityRole, DiscordID: "role-1"}},
		Apply: func(ctx context.Context) (string, error) {
			dependentApplied = true
			return "stoat-chan-1", nil
		},
	}
	dependency := Op{
		Kind:       OpCreate,
		EntityType: EntityRole,
		DiscordID:  "role-1",
		Apply: func(ctx context.Context) (string, error) {
			return "stoat-role-1", nil
		},
	}

	e.Submit(dependent)
	e.Submit(dependency)
	e.Wait()

	if !dependentApplied {
		t.Fatal("expected dependent op to run after its dependency confirmed")
	}
	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.Status != StatusActive {
		t.Fatalf("expected dependent channel mapping confirmed active, got %+v", m)
	}
}

func TestSubmit_MultiDependencyOp_WaitsForAllDependenciesBeforeApplying(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	appliedCh := make(chan struct{})
	dependent := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "overwrite-1",
		DependsOn: []DependencyKey{
			{EntityType: EntityRole, DiscordID: "role-1"},
			{EntityType: EntityChannel, DiscordID: "chan-1"},
		},
		Apply: func(ctx context.Context) (string, error) {
			close(appliedCh)
			return "", nil
		},
	}
	role := Op{
		Kind:       OpCreate,
		EntityType: EntityRole,
		DiscordID:  "role-1",
		Apply:      func(ctx context.Context) (string, error) { return "stoat-role-1", nil },
	}

	e.Submit(dependent)
	e.Submit(role)

	select {
	case <-appliedCh:
		t.Fatal("expected dependent op to stay deferred until the second dependency (chan-1) also lands")
	case <-time.After(50 * time.Millisecond):
		// expected: still waiting on chan-1
	}

	channel := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply:      func(ctx context.Context) (string, error) { return "stoat-chan-1", nil },
	}
	e.Submit(channel)
	e.Wait()

	select {
	case <-appliedCh:
	default:
		t.Fatal("expected dependent op to apply once both dependencies landed")
	}
}

func TestSubmit_MessageOps_SameChannelRunInSubmitOrder(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	var mu sync.Mutex
	var order []string

	makeOp := func(id string) Op {
		return Op{
			Kind:       OpCreate,
			EntityType: EntityMessage,
			DiscordID:  id,
			ChannelID:  "chan-1",
			Apply: func(ctx context.Context) (string, error) {
				mu.Lock()
				order = append(order, id)
				mu.Unlock()
				return "stoat-" + id, nil
			},
		}
	}

	e.Submit(makeOp("msg-1"))
	e.Submit(makeOp("msg-2"))
	e.Submit(makeOp("msg-3"))
	e.Wait()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"msg-1", "msg-2", "msg-3"}
	if len(order) != len(want) {
		t.Fatalf("expected %d applies, got %d: %v", len(want), len(order), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected send-order == submit-order (spec 5), got %v", order)
		}
	}
}

func TestSubmit_MessageOps_DifferentChannelsDoNotHeadOfLineBlock(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	blockA := make(chan struct{})
	appliedB := make(chan struct{})

	opA := Op{
		Kind:       OpCreate,
		EntityType: EntityMessage,
		DiscordID:  "msg-a",
		ChannelID:  "chan-a",
		Apply: func(ctx context.Context) (string, error) {
			<-blockA
			return "stoat-a", nil
		},
	}
	opB := Op{
		Kind:       OpCreate,
		EntityType: EntityMessage,
		DiscordID:  "msg-b",
		ChannelID:  "chan-b",
		Apply: func(ctx context.Context) (string, error) {
			close(appliedB)
			return "stoat-b", nil
		},
	}

	e.Submit(opA)
	e.Submit(opB)

	select {
	case <-appliedB:
	case <-time.After(time.Second):
		t.Fatal("expected channel B's op to run without waiting on channel A's blocked op (no head-of-line blocking across channels, spec 5)")
	}

	close(blockA)
	e.Wait()
}

// recordingRateLimiter is a fake RateLimiter for engine tests -- unlike
// fakeRateLimiter it never blocks but records what the engine asked of it,
// so retry-on-429 behavior is directly assertable.
type recordingRateLimiter struct {
	waitCalls    int
	backoffCalls []float64
}

func (r *recordingRateLimiter) Wait(ctx context.Context, bucketKey string) error {
	r.waitCalls++
	return nil
}

func (r *recordingRateLimiter) Backoff(bucketKey string, retryAfterSeconds float64) {
	r.backoffCalls = append(r.backoffCalls, retryAfterSeconds)
}

func TestRunRemote_RetriesOn429AndBacksOffLimiter(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	limiter := &recordingRateLimiter{}
	e := New(mappings, health, queue, limiter, nil)

	attempts := 0
	op := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply: func(ctx context.Context) (string, error) {
			attempts++
			if attempts < 3 {
				return "", &RateLimitedError{RetryAfterSeconds: 1.5}
			}
			return "stoat-chan-1", nil
		},
	}

	e.Submit(op)
	e.Wait()

	if attempts != 3 {
		t.Fatalf("expected 3 attempts (2 rate-limited + 1 success), got %d", attempts)
	}
	if len(limiter.backoffCalls) != 2 || limiter.backoffCalls[0] != 1.5 || limiter.backoffCalls[1] != 1.5 {
		t.Fatalf("expected 2 Backoff(1.5) calls, got %v", limiter.backoffCalls)
	}
	if limiter.waitCalls != 3 {
		t.Fatalf("expected Wait called before each attempt, got %d", limiter.waitCalls)
	}
	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.Status != StatusActive {
		t.Fatalf("expected eventual success confirmed active, got %+v", m)
	}
}

func TestRunRemote_GivesUpAfterMaxRetries(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	limiter := &recordingRateLimiter{}
	e := New(mappings, health, queue, limiter, nil)

	attempts := 0
	op := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply: func(ctx context.Context) (string, error) {
			attempts++
			return "", &RateLimitedError{RetryAfterSeconds: 0.1}
		},
	}

	e.Submit(op)
	e.Wait()

	if attempts != maxRemoteRetries+1 {
		t.Fatalf("expected exactly maxRemoteRetries+1 attempts, got %d", attempts)
	}
	m, _ := mappings.Get(string(EntityChannel), "chan-1")
	if m.Status == StatusActive {
		t.Fatal("expected op to remain unconfirmed after exhausting retries")
	}
}

// TestSubmit_SameEntityOpsAreSerialized guards against the race that caused
// a live 404: a channel's own create and a follow-up edit ran concurrently,
// so the edit read the create's still-pending mapping row (Found=true,
// StoatID="") and sent Stoat a request with an empty channel id. Ops for
// the same entity must fully serialize regardless of entity type.
func TestSubmit_SameEntityOpsAreSerialized(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	createStarted := make(chan struct{})
	releaseCreate := make(chan struct{})
	var mu sync.Mutex
	var order []string

	create := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply: func(ctx context.Context) (string, error) {
			close(createStarted)
			<-releaseCreate
			mu.Lock()
			order = append(order, "create")
			mu.Unlock()
			return "stoat-chan-1", nil
		},
	}
	edit := Op{
		Kind:       OpUpdate,
		EntityType: EntityChannel,
		DiscordID:  "chan-1",
		Apply: func(ctx context.Context) (string, error) {
			mu.Lock()
			order = append(order, "edit")
			mu.Unlock()
			return "stoat-chan-1", nil
		},
	}

	e.Submit(create)
	<-createStarted // create's worker has picked it up and is mid-flight
	e.Submit(edit)
	close(releaseCreate)
	e.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "create" || order[1] != "edit" {
		t.Fatalf("expected create to fully apply before edit started, got %v", order)
	}
}

// TestSubmit_DifferentEntitiesStillRunInParallel guards against
// accidentally over-serializing: only same-entity ops should share a
// worker. Two different channels' ops must not head-of-line block.
func TestSubmit_DifferentEntitiesStillRunInParallel(t *testing.T) {
	mappings := newFakeMappingStore()
	health := &fakeHealthChecker{ok: true}
	queue := &fakeQueueStore{}
	e := newTestEngine(mappings, health, queue)

	blockA := make(chan struct{})
	appliedB := make(chan struct{})

	opA := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-a",
		Apply: func(ctx context.Context) (string, error) {
			<-blockA
			return "stoat-a", nil
		},
	}
	opB := Op{
		Kind:       OpCreate,
		EntityType: EntityChannel,
		DiscordID:  "chan-b",
		Apply: func(ctx context.Context) (string, error) {
			close(appliedB)
			return "stoat-b", nil
		},
	}

	e.Submit(opA)
	e.Submit(opB)

	select {
	case <-appliedB:
	case <-time.After(time.Second):
		t.Fatal("chan-b's op was head-of-line blocked behind chan-a's still-blocked op")
	}

	close(blockA)
	e.Wait()
}
