package storagetest

import (
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// sortJobsDesc sorts in-place by (CreatedAt DESC, ID DESC). Used by the
// memoryStore's ListJobsCursor implementation so the iteration order matches
// the SQL backends (Postgres ORDER BY ... DESC, SQLite ORDER BY ... DESC).
func sortJobsDesc(jobs []storage.JobRecord) {
	sort.Slice(jobs, func(i, j int) bool {
		if !jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
		}
		return jobs[i].ID > jobs[j].ID
	})
}

// sortAuditDesc is the audit-record analogue of sortJobsDesc.
func sortAuditDesc(events []storage.AuditEventRecord) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].CreatedAt.After(events[j].CreatedAt)
		}
		return events[i].ID > events[j].ID
	})
}

// jobAfterCursor reports whether (job.CreatedAt, job.ID) is strictly less
// than (cursorCreatedAt, cursorID) under the (created_at DESC, id DESC)
// ordering used by ListJobsCursor. "Strictly less" is what we want because
// the cursor itself was the LAST row of the previous page; we must not
// re-emit it.
func jobAfterCursor(job storage.JobRecord, cursorCreatedAt time.Time, cursorID string) bool {
	if job.CreatedAt.Before(cursorCreatedAt) {
		return true
	}
	if job.CreatedAt.After(cursorCreatedAt) {
		return false
	}
	return job.ID < cursorID
}

// auditAfterCursor mirrors jobAfterCursor for AuditEventRecord.
func auditAfterCursor(event storage.AuditEventRecord, cursorCreatedAt time.Time, cursorID string) bool {
	if event.CreatedAt.Before(cursorCreatedAt) {
		return true
	}
	if event.CreatedAt.After(cursorCreatedAt) {
		return false
	}
	return event.ID < cursorID
}
