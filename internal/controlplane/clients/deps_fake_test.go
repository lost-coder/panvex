package clients

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// fakeDeps is a settable, call-recording test double for Deps. Modeled on
// fakeRepo from internal/controlplane/configtargets/service_test.go
// (P8.2f). No test functions live here yet — Tasks 3-4 wire these up.
type fakeDeps struct {
	// Settable inputs.
	topology        AgentTopology
	readOnly        map[string]bool
	fleetGroupByAgt map[string]string
	ttl             time.Duration

	// Recorded calls.
	notified       [][]string
	publishedJobs  []jobs.Job
	updatedClients []ClientID
}

func (f *fakeDeps) Topology() AgentTopology {
	return f.topology
}

func (f *fakeDeps) ReadOnlyAgents(agentIDs []string) map[string]bool {
	result := make(map[string]bool, len(agentIDs))
	for _, id := range agentIDs {
		if ro, ok := f.readOnly[id]; ok {
			result[id] = ro
		}
	}
	return result
}

func (f *fakeDeps) AgentFleetGroupID(agentID string) (string, bool) {
	groupID, ok := f.fleetGroupByAgt[agentID]
	return groupID, ok
}

func (f *fakeDeps) NotifyAgentSessions(agentIDs []string) {
	f.notified = append(f.notified, agentIDs)
}

func (f *fakeDeps) PublishJobCreated(job jobs.Job) {
	f.publishedJobs = append(f.publishedJobs, job)
}

func (f *fakeDeps) PublishClientsUpdated(clientID ClientID) {
	f.updatedClients = append(f.updatedClients, clientID)
}

func (f *fakeDeps) ClientJobTTL() time.Duration {
	return f.ttl
}

var _ Deps = (*fakeDeps)(nil)

// fakeJobQueue is a settable, call-recording test double for JobQueue.
type fakeJobQueue struct {
	// Settable inputs.
	enqueueJob jobs.Job
	enqueueErr error
	getJobs    map[string]jobs.Job

	// Recorded calls.
	enqueued []jobs.CreateJobInput
}

func (f *fakeJobQueue) Enqueue(_ context.Context, input jobs.CreateJobInput, _ time.Time) (jobs.Job, error) {
	f.enqueued = append(f.enqueued, input)
	if f.enqueueErr != nil {
		return jobs.Job{}, f.enqueueErr
	}
	return f.enqueueJob, nil
}

func (f *fakeJobQueue) Get(jobID string) (jobs.Job, bool) {
	job, ok := f.getJobs[jobID]
	return job, ok
}

var _ JobQueue = (*fakeJobQueue)(nil)

// Compile-time proof that *jobs.Service satisfies JobQueue is asserted in
// the non-test file deps.go (safe: jobs does not import clients).
