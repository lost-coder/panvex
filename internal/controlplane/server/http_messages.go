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
	msgConfigApplyBatchNotFound   = "config-apply batch not found"

	// Client-mutation error allowlist (3.10): handleClientMutationError maps
	// every known sentinel from the client validation/mutation path to one
	// of these fixed, operator-safe strings instead of echoing err.Error()
	// verbatim. This closes the raw-error-leak gap where a future change to
	// an underlying wrapped error (e.g. a storage driver adding context to
	// its error text) could leak internal detail through the HTTP response
	// without anyone noticing — scrubErrorMessage's denylist only catches a
	// handful of keywords, not stack/detail-shaped leaks in general. Each
	// message intentionally mirrors the wording of its sentinel so behaviour
	// is unchanged for well-formed requests.
	msgClientNameRequired    = "client name is required"
	msgClientNameInvalid     = "client name must match [A-Za-z0-9_.-] and be 1..64 chars"
	msgClientUserADTag       = "user_ad_tag must contain exactly 32 hex characters"
	msgClientExpiration      = "expiration_rfc3339 must be a valid RFC3339 timestamp"
	msgClientTargetsRequired = "client must target at least one agent"
	msgClientLimitNegative   = "max_tcp_conns, max_unique_ips and data_quota_bytes must be >= 0"
	msgClientReadOnlyTarget  = "job targets read-only telemt instance"

	logBatchFlush         = "batch flush"
	logAgentStreamClosed  = "agent stream closed"
	logMessageReceived    = "message received"
	auditJobsResult       = "jobs.result"
	auditJobsAcknowledged = "jobs.acknowledged"
	auditAgentsCertRenewed = "agents.cert_renewed"
)
