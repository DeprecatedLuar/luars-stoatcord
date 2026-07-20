package engine

import (
	"fmt"
	"log/slog"
	"sync"
)

// maxRemoteRetries bounds how many times a single op retries a 429 before
// giving up and logging (no magic values).
const maxRemoteRetries = 5

// channelWorkerBuffer is the per-channel op queue depth: large enough that
// a burst on one channel doesn't drop ops, small enough that a stuck worker
// surfaces (Submit blocks past this, visible in goroutine backpressure)
// rather than growing unbounded.
const channelWorkerBuffer = 64

// Engine is the orchestrator (spec: health-check -> diff -> write-intent ->
// remote -> confirm). It holds zero translation logic; Op.Diff/Op.Apply
// supply that per call.
type Engine struct {
	mappings MappingStore
	health   HealthChecker
	queue    QueueStore
	limiter  RateLimiter
	log      *slog.Logger

	wg sync.WaitGroup

	workersMu sync.Mutex
	workers   map[string]chan Op

	waitersMu sync.Mutex
	waiters   map[DependencyKey][]Op
}

// New constructs an Engine. log may be nil in tests that don't assert on
// logging.
func New(mappings MappingStore, health HealthChecker, queue QueueStore, limiter RateLimiter, log *slog.Logger) *Engine {
	return &Engine{
		mappings: mappings,
		health:   health,
		queue:    queue,
		limiter:  limiter,
		log:      log,
		workers:  map[string]chan Op{},
		waiters:  map[DependencyKey][]Op{},
	}
}

// Submit is the single public door: callers never see the internal
// per-key worker or dependency-waiter maps. Every op routes to a
// serialized worker keyed by workerKey: message ops share a worker per
// channel (send-order = display-order, spec 5); every other op shares a
// worker per entity, so a create and its own follow-up edit/delete for the
// same Discord id can never race each other into using an unconfirmed
// mapping. Different entities still get their own workers and proceed in
// parallel.
func (e *Engine) Submit(op Op) {
	e.wg.Add(1)
	e.workerFor(workerKey(op)) <- op
}

// workerKey picks the serialization key for an op (see Submit).
func workerKey(op Op) string {
	if op.EntityType == EntityMessage && op.ChannelID != "" {
		return "message:" + op.ChannelID
	}
	return string(op.EntityType) + ":" + op.DiscordID
}

// Wait blocks until every submitted op (including any currently deferred on
// a dependency) has finished processing. Intended for tests and graceful
// shutdown, not the live per-op path.
func (e *Engine) Wait() {
	e.wg.Wait()
}

func (e *Engine) workerFor(key string) chan Op {
	e.workersMu.Lock()
	defer e.workersMu.Unlock()

	ch, ok := e.workers[key]
	if ok {
		return ch
	}
	ch = make(chan Op, channelWorkerBuffer)
	e.workers[key] = ch
	go func() {
		for op := range ch {
			e.tryRun(op)
		}
	}()
	return ch
}

// tryRun checks an op's dependencies and either processes it now or defers
// it into the waiters index, keyed by the first unmet dependency. Retries
// from notifyWaiters call back into tryRun without re-adding to wg, since
// the op is still the same in-flight unit of work started by Submit.
func (e *Engine) tryRun(op Op) {
	if key, unmet := e.firstUnmetDependency(op); unmet {
		e.deferOp(op, key)
		return
	}
	e.process(op)
	e.wg.Done()
}

func (e *Engine) firstUnmetDependency(op Op) (DependencyKey, bool) {
	for _, dep := range op.DependsOn {
		mapping, err := e.mappings.Get(string(dep.EntityType), dep.DiscordID)
		if err != nil || !mapping.Found || mapping.Status != StatusActive {
			return dep, true
		}
	}
	return DependencyKey{}, false
}

func (e *Engine) deferOp(op Op, waitingOn DependencyKey) {
	e.waitersMu.Lock()
	e.waiters[waitingOn] = append(e.waiters[waitingOn], op)
	e.waitersMu.Unlock()

	if e.log != nil {
		e.log.Warn("engine: op deferred, waiting on dependency",
			"entity_type", op.EntityType, "discord_id", op.DiscordID,
			"waiting_on_entity_type", waitingOn.EntityType, "waiting_on_discord_id", waitingOn.DiscordID)
	}
}

// notifyWaiters re-attempts only the ops waiting on the dependency that just
// landed (O(1) via the waiters index key, no full scan, spec 5).
func (e *Engine) notifyWaiters(entityType EntityType, discordID string) {
	key := DependencyKey{EntityType: entityType, DiscordID: discordID}

	e.waitersMu.Lock()
	waiting := e.waiters[key]
	delete(e.waiters, key)
	e.waitersMu.Unlock()

	for _, op := range waiting {
		e.workerFor(workerKey(op)) <- op
	}
}

func (e *Engine) process(op Op) {
	ok, degraded := e.health.Check()
	if !ok {
		if err := e.queue.Enqueue(opQueueType(op), op.CanonicalState); err != nil && e.log != nil {
			e.log.Error("engine: failed to enqueue op while unhealthy", "error", err)
		}
		if e.log != nil {
			e.log.Warn("engine: degraded, op enqueued for later drain",
				"entity_type", op.EntityType, "discord_id", op.DiscordID, "degraded", degraded)
		}
		return
	}

	if op.Kind == OpDelete {
		if _, err := e.runRemote(op); err != nil {
			e.logRemoteError(op, err)
			return
		}
		if err := e.mappings.Remove(string(op.EntityType), op.DiscordID); err != nil && e.log != nil {
			e.log.Error("engine: failed to remove mapping after delete", "error", err)
		}
		return
	}

	mapping, err := e.mappings.Get(string(op.EntityType), op.DiscordID)
	if err != nil {
		if e.log != nil {
			e.log.Error("engine: failed to read mapping", "error", err)
		}
		return
	}

	if mapping.Found && op.Diff != nil {
		equal, err := op.Diff(mapping.CanonicalState)
		if err != nil {
			if e.log != nil {
				e.log.Error("engine: diff failed", "error", err)
			}
			return
		}
		if equal {
			return
		}
	}

	if err := e.mappings.WritePending(string(op.EntityType), op.DiscordID, op.CanonicalState); err != nil {
		if e.log != nil {
			e.log.Error("engine: failed to write pending row", "error", err)
		}
		return
	}

	stoatID, err := e.runRemote(op)
	if err != nil {
		e.logRemoteError(op, err)
		return
	}

	if err := e.mappings.Confirm(string(op.EntityType), op.DiscordID, stoatID); err != nil {
		if e.log != nil {
			e.log.Error("engine: failed to confirm mapping", "error", err)
		}
		return
	}

	e.notifyWaiters(op.EntityType, op.DiscordID)
}

func (e *Engine) logRemoteError(op Op, err error) {
	if e.log == nil {
		return
	}
	e.log.Error("engine: remote apply failed", "entity_type", op.EntityType, "discord_id", op.DiscordID, "error", err)
}

func opQueueType(op Op) string {
	return fmt.Sprintf("%s:%d", op.EntityType, op.Kind)
}
