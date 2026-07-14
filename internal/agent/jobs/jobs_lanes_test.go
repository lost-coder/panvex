package jobs

import "testing"

// TestJobWorkerCountIsOnePerLane pins the lane design: every pipeline is
// single-worker so jobs within a lane apply strictly in delivery order and
// runtime.reload can never race telemetry.refresh_diagnostics against the
// same local Telemt admin API (audit minor finding, Plan 4 Task 3).
func TestJobWorkerCountIsOnePerLane(t *testing.T) {
	for _, pipeline := range []Pipeline{
		PipelineRuntimeReload,
		PipelineClientMutation,
		PipelineDefault,
	} {
		if got := workerCountForPipeline(pipeline); got != 1 {
			t.Fatalf("workerCountForPipeline(%s) = %d, want 1", pipeline, got)
		}
	}
}
