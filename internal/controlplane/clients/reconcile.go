package clients

import (
	"context"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// Client-state delivery is convergent: a node that was offline, restarting, or
// simply unreachable when an operator changed a client eventually receives that
// change anyway, without anyone pressing Redeploy.
//
// It did not use to be. A client job carries a 10-minute TTL and, once expired,
// was never re-sent: the only producer of client jobs was the operator flow that
// created them. A node offline for eleven minutes during a client DELETE kept
// serving that client's credentials forever, while the panel showed the client
// as deleted. That is a revocation hole, not a cosmetic drift — see
// docs/audit/2026-07-07-rollout-restart-diagnosis.md (C1/R-2).
//
// The fix reuses the whole existing job path rather than inventing a second one.
// A deployment row already records what each node is SUPPOSED to have
// (DesiredOperation) and whether it confirmed it (Status). The reconciler walks
// those rows and re-enqueues an ordinary job for any that the node has not
// confirmed. Supersede-by-client collapses duplicates, and the agent's
// idempotency cache absorbs replays, so re-enqueueing is cheap and safe.

// clientReconcileInterval is the safety-net tick. Reconnect and boot are the
// triggers that matter; this one exists so a node that never drops its stream
// but somehow missed a job still converges.
const clientReconcileInterval = 10 * time.Minute

// clientJobTTLDefault is the compiled-in default client-job TTL used to throttle
// the reconciler. The live, operator-tunable TTL stays server-side (resolved via
// server.effectiveClientJobTTL); the reconciler's throttle keys off the
// compiled-in default so re-sends never outrun the window a node has to answer.
const clientJobTTLDefault = 10 * time.Minute

// clientReconcileMinInterval throttles how often the SAME (client, agent) pair
// may be re-enqueued. It matches the job TTL: re-sending sooner than that just
// supersedes a job the node has not had time to answer yet.
const clientReconcileMinInterval = clientJobTTLDefault

// clientReconcileWarnAfter is how many consecutive re-enqueues for one pair go
// by before we say so out loud. A node that keeps failing the same job is not
// something to retry silently forever.
const clientReconcileWarnAfter = 5

// reconciler tracks per-pair attempts so the reconciler neither storms a
// node nor loops silently.
type reconciler struct {
	mu       sync.Mutex
	lastTry  map[string]time.Time
	attempts map[string]int
}

func newReconciler() *reconciler {
	return &reconciler{
		lastTry:  make(map[string]time.Time),
		attempts: make(map[string]int),
	}
}

// shouldRetry reports whether the pair may be re-enqueued now, and returns the
// attempt count so the caller can log a persistently-failing pair.
func (r *reconciler) shouldRetry(key string, now time.Time) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if last, ok := r.lastTry[key]; ok && now.Sub(last) < clientReconcileMinInterval {
		return 0, false
	}
	r.lastTry[key] = now
	r.attempts[key]++
	return r.attempts[key], true
}

// confirm clears the bookkeeping for a pair the node has confirmed. Called from
// the job-result path so a converged deployment starts from a clean slate if it
// ever diverges again.
func (r *reconciler) confirm(key string) {
	r.mu.Lock()
	delete(r.lastTry, key)
	delete(r.attempts, key)
	r.mu.Unlock()
}

func reconcileKey(clientID ClientID, agentID string) string {
	return string(clientID) + "|" + agentID
}

// StartReconcileWorker runs the safety-net tick until ctx is cancelled, after
// one immediate pass. The immediate pass IS the boot trigger: it picks up
// deployments left queued by a panel that died between persisting client state
// and enqueueing its job, and every job that expired while the panel was down.
//
// The caller owns wg.Add(1); the worker Done()s on exit.
func (s *Service) StartReconcileWorker(ctx context.Context, interval time.Duration, wg *sync.WaitGroup) {
	if interval <= 0 {
		interval = clientReconcileInterval
	}
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		s.ReconcileDeployments(ctx, "")

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ReconcileDeployments(ctx, "")
			}
		}
	}()
}

// ReconcileDeployments re-enqueues a job for every deployment whose node
// has not confirmed its desired operation. agentFilter, when non-empty, limits
// the pass to one node — that is the reconnect trigger, which is the one that
// actually closes the offline-during-delete hole.
//
// Returns the number of jobs enqueued (used by tests).
func (s *Service) ReconcileDeployments(ctx context.Context, agentFilter string) int {
	now := s.now().UTC()
	mirror := s.MirrorSnapshot()

	enqueued := 0
	for clientID, byAgent := range mirror.Deployments {
		client, ok := mirror.Clients[clientID]
		if !ok {
			// A deployment with no client row at all: nothing to send. The
			// discovery path is what surfaces an orphan like this to the
			// operator; re-enqueueing a job for a client we cannot describe
			// would just fail on the node.
			continue
		}

		// Group the divergent agents by the action they are owed, so one client
		// produces at most one job per action rather than one job per node.
		pending := map[jobs.Action][]string{}
		for agentID, deployment := range byAgent {
			if agentFilter != "" && agentID != agentFilter {
				continue
			}
			action, ok := divergentDeploymentAction(deployment)
			if !ok {
				continue
			}
			attempt, allowed := s.reconcile.shouldRetry(reconcileKey(clientID, agentID), now)
			if !allowed {
				continue
			}
			if attempt >= clientReconcileWarnAfter {
				s.logger.WarnContext(ctx, "client deployment still unconfirmed after repeated re-enqueues",
					"client_id", string(clientID),
					"agent_id", agentID,
					"desired_operation", deployment.DesiredOperation,
					"status", deployment.Status,
					"attempts", attempt,
				)
			}
			pending[action] = append(pending[action], agentID)
		}

		for action, agentIDs := range pending {
			// R10-1: the snapshot above may be stale by the time we get here —
			// an operator rotate/update that committed in between would be
			// silently undone on the node if this job shipped the snapshot's
			// payload, because a job created LATER supersedes the operator's
			// fresher one (supersede is CreatedAt-ordered, keyed by client_id)
			// and the node then converges on the stale secret with
			// Status=succeeded. Re-read the client from the mirror immediately
			// before each enqueue so the payload always carries the newest
			// committed state. The snapshot still decides WHICH pairs are owed
			// a re-send; only the payload must be fresh. Lock discipline: Get
			// takes and releases s.mu internally — s.mu is never held across
			// EnqueueClientJob (enqueue/publish/audit).
			freshClient, err := s.Get(ctx, clientID)
			if err != nil {
				// The client vanished between snapshot and enqueue: nothing to
				// describe to the node. Same reasoning as the missing-row skip
				// at the top of the loop.
				continue
			}
			if freshClient.DeletedAt != nil && client.DeletedAt == nil {
				// Tombstoned since the snapshot: DeleteFlow already enqueued
				// the delete job, and shipping the snapshot's stale non-delete
				// action now would supersede it (resurrecting the client on
				// the node). The next pass re-sends the delete if the node
				// leaves it unconfirmed.
				continue
			}
			if _, err := s.EnqueueClientJob(ctx, actorReconciler, action, freshClient, agentIDs, now); err != nil {
				s.logger.ErrorContext(ctx, "re-enqueue of unconfirmed client job failed",
					"client_id", string(clientID),
					"action", string(action),
					"agents", len(agentIDs),
					"error", err,
				)
				continue
			}
			enqueued++
		}
	}

	return enqueued
}

// actorReconciler is the audit actor for jobs the panel re-sends on its own.
// Distinct from a user ID so the audit trail never attributes an automatic
// retry to the operator who made the original change.
const actorReconciler = "system:reconciler"

// divergentDeploymentAction reports the job a node still owes us for this
// deployment, if any.
//
// A succeeded deployment is converged — nothing to do. A queued one was never
// confirmed (the node was offline, or the job expired before it connected). A
// failed one WAS answered by the node, so we re-send it only when the desired
// operation is a revocation (delete or secret rotation): leaving stale
// credentials live on a node because one apply failed is the exact hole this
// exists to close, whereas re-sending a failing create forever would just churn.
func divergentDeploymentAction(deployment Deployment) (jobs.Action, bool) {
	action := jobs.Action(deployment.DesiredOperation)
	switch action {
	case jobs.ActionClientCreate, jobs.ActionClientUpdate, jobs.ActionClientDelete, jobs.ActionClientRotateSecret:
	default:
		return "", false
	}

	switch deployment.Status {
	case DeploymentStatusSucceeded:
		return "", false
	case DeploymentStatusQueued, DeploymentStatusAwaitingNode:
		// awaiting_node is a queued job whose TTL lapsed unconfirmed — the same
		// re-send class as queued: the node still owes us this operation.
		return action, true
	case DeploymentStatusFailed:
		if action == jobs.ActionClientDelete || action == jobs.ActionClientRotateSecret {
			return action, true
		}
		return "", false
	default:
		return "", false
	}
}

// ReconcileTopology recomputes each client's target nodes and dispatches
// the jobs the change implies: an update (create, in effect) to nodes that just
// became targets, a delete to nodes that stopped being targets.
//
// Before R10 a topology change produced no jobs at all. Moving an agent between
// fleet groups left the clients of both groups untouched on the node until an
// operator happened to edit each of them — the panel said the client was
// deployed to the group, the node had never heard of it, and the reverse for
// the group the agent left (C4).
//
// It is deliberately whole-fleet rather than scoped to the agent that moved:
// a group change can add and remove targets for many clients at once, and the
// mirror walk is cheap next to the round-trip it saves.
//
// R10-2: the whole-fleet snapshot only PRE-FILTERS which clients might
// diverge. The actual read-compute-write per client runs inside
// reconcileTopologyClient under that client's save lock, so a concurrent
// operator mutation (e.g. a secret rotation) committed after the snapshot is
// never overwritten by a full-row save built from stale data.
func (s *Service) ReconcileTopology(ctx context.Context, actorID string, observedAt time.Time) {
	mirror := s.MirrorSnapshot()

	for clientID, client := range mirror.Clients {
		if client.DeletedAt != nil {
			// A tombstoned client's targets never grow. Its outstanding
			// deletes are the reconciler's business (above), not topology's.
			continue
		}
		assignments := mirror.Assignments[clientID]
		deployments := deploymentsForClient(mirror, clientID)

		targetAgentIDs := s.ResolveTargetAgentIDs(assignments, s.deps.Topology())
		if sameAgentSet(DeploymentAgentIDs(deployments), targetAgentIDs) {
			continue
		}

		freshClient, freshDeployments, freshTargets, saved, err := s.reconcileTopologyClient(ctx, clientID, observedAt)
		if err != nil {
			s.logger.ErrorContext(ctx, "persist client state after topology change failed",
				"client_id", string(clientID), "error", err)
			continue
		}
		if !saved {
			// Vanished, tombstoned, or no longer divergent on fresh state —
			// nothing to persist for this client.
			continue
		}
		s.deps.PublishClientsUpdated(freshClient.ID)
		if err := s.DispatchClientUpdateJobs(ctx, actorID, freshClient, freshDeployments, freshTargets, observedAt); err != nil {
			s.logger.ErrorContext(ctx, "dispatch client jobs after topology change failed",
				"client_id", string(clientID), "error", err)
		}
	}
}

// reconcileTopologyClient re-reads one client's state from the mirror,
// re-checks that its topology actually diverges, and persists the rebuilt
// deployments — all under the client's save lock, so the write is based on
// the state current at write time (R10-2: the fleet snapshot in
// ReconcileTopology is stale by the time the pass reaches a client, and a
// full-row SaveState built from it silently reverts any operator mutation —
// e.g. a secret rotation — committed in between).
//
// Lock discipline: operator save paths hold clientSaveLock across
// commit + mirror-apply (SaveState), so under this lock the mirror is the
// committed truth for this client — every concurrent save either finished
// before the re-read (and is included) or starts after this critical section
// (and wins as the later writer, with its later-created jobs superseding the
// ones dispatched here). deps.Topology() is called by our caller BEFORE the
// lock is taken (it acquires server-side locks; a client save lock must never
// wait on those), and the mirror reads/writes inside take s.mu themselves —
// the documented saveLock → uow → s.mu ordering is preserved, and the lock is
// released before any publish/enqueue.
//
// Returns saved=false when the client vanished, was tombstoned, or no longer
// diverges on fresh state. On success it returns the fresh client, its
// PRE-rebuild deployments (DispatchClientUpdateJobs derives the delete
// targets from them), and the fresh target set.
func (s *Service) reconcileTopologyClient(ctx context.Context, clientID ClientID, observedAt time.Time) (Client, []Deployment, []string, bool, error) {
	topology := s.deps.Topology()

	saveLock := s.clientSaveLock(clientID)
	saveLock.Lock()
	defer saveLock.Unlock()

	client, assignments, deployments, err := s.detailSnapshot(string(clientID))
	if err != nil || client.DeletedAt != nil {
		// Vanished or tombstoned between the fleet snapshot and now: the
		// flow that removed it owns the follow-up jobs.
		return Client{}, nil, nil, false, nil
	}
	targetAgentIDs := s.ResolveTargetAgentIDs(assignments, topology)
	if sameAgentSet(DeploymentAgentIDs(deployments), targetAgentIDs) {
		// The divergence seen on the stale snapshot is already resolved.
		return Client{}, nil, nil, false, nil
	}

	next := BuildDeployments(deployments, clientID, targetAgentIDs, string(jobs.ActionClientUpdate), string(jobs.ActionClientDelete), observedAt)
	if s.HasRepo() {
		if err := s.saveStateHoldingSaveLock(ctx, client, assignments, next); err != nil {
			return Client{}, nil, nil, false, err
		}
	} else {
		// No-repo fallback (test doubles / pre-migrate stores), mirroring
		// saveStateAndPublish's split.
		s.MirrorReplaceInMemory(client, assignments, next)
	}
	return client, deployments, targetAgentIDs, true, nil
}

func deploymentsForClient(mirror MirrorState, clientID ClientID) []Deployment {
	byAgent := mirror.Deployments[clientID]
	out := make([]Deployment, 0, len(byAgent))
	for _, deployment := range byAgent {
		out = append(out, deployment)
	}
	return out
}

// sameAgentSet reports whether two agent-ID lists describe the same set,
// ignoring order and duplicates.
func sameAgentSet(left, right []string) bool {
	set := make(map[string]struct{}, len(left))
	for _, id := range left {
		set[id] = struct{}{}
	}
	for _, id := range right {
		if _, ok := set[id]; !ok {
			return false
		}
		delete(set, id)
	}
	return len(set) == 0
}
