package server

// Shared HTTP response message constants. Extracted to avoid duplicating
// the same literal across many handlers (Sonar S1192) and to keep the
// wire-visible error vocabulary consistent.
const (
	msgInternalError              = "internal error"
	msgClientNotFound             = "client not found"
	msgClientScopeCheckFailed     = "load client for scope check failed"
	msgDiscoveredClientNotFound   = "discovered client not found"
	msgEnrollmentTokenNotFound    = "enrollment token not found"
	msgIntegrationNotFound        = "integration not found"
	msgProviderIDRequired         = "provider id is required"
	msgProviderNotFound           = "provider not found"
	msgIntegrationIDRequired      = "integration id is required"
	msgAgentNotFound              = "agent not found"
	msgStorageError               = "storage error"
	msgServerNotFound             = "server not found"
	msgPersistentStoreRequired    = "persistent store required"
	msgRecoveryNotAllowed         = "agent certificate recovery is not allowed"
	msgAdminRoleRequired          = "admin role required"
	msgClientIDRequired           = "client id is required"

	logBatchFlush         = "batch flush"
	logAgentStreamClosed  = "agent stream closed"
	logMessageReceived    = "message received"
	auditJobsResult       = "jobs.result"
	auditJobsAcknowledged = "jobs.acknowledged"
	auditAgentsCertRenewed = "agents.cert_renewed"
)
