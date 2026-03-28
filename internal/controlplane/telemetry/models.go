package telemetry

// Freshness stores normalized runtime freshness information for operator projections.
type Freshness struct {
	State          string
	ObservedAtUnix int64
}

// DetailBoost stores normalized detail boost state for operator projections.
type DetailBoost struct {
	Active           bool
	ExpiresAtUnix    int64
	RemainingSeconds int64
}
