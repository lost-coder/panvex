package postgres

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env var names for tuning the database/sql connection pool. Defaults below
// were sized to support ~50 concurrent agents on a single CP replica without
// hitting `connection pool exhausted`. See docs/REMEDIATION_PLAN.md §0.7.
const (
	EnvMaxOpenConns    = "PANVEX_DB_MAX_OPEN_CONNS"
	EnvMaxIdleConns    = "PANVEX_DB_MAX_IDLE_CONNS"
	EnvConnMaxLifetime = "PANVEX_DB_CONN_MAX_LIFETIME"
	EnvConnMaxIdleTime = "PANVEX_DB_CONN_MAX_IDLE_TIME"
)

const (
	defaultMaxOpenConns    = 50
	defaultMaxIdleConns    = 20
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 10 * time.Minute
)

// errInvalidPoolEnv wraps every parse/validation failure so callers (and
// tests) can recognise misconfiguration regardless of which knob was wrong.
var errInvalidPoolEnv = errors.New("invalid postgres pool env var")

// PoolConfig captures the four knobs database/sql exposes for connection
// pool sizing. Zero values are not valid: an unset or empty env var falls
// back to the package defaults via loadPoolConfigFromEnv.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func loadPoolConfigFromEnv() (PoolConfig, error) {
	cfg := PoolConfig{
		MaxOpenConns:    defaultMaxOpenConns,
		MaxIdleConns:    defaultMaxIdleConns,
		ConnMaxLifetime: defaultConnMaxLifetime,
		ConnMaxIdleTime: defaultConnMaxIdleTime,
	}

	if v, err := positiveIntEnv(EnvMaxOpenConns); err != nil {
		return PoolConfig{}, err
	} else if v > 0 {
		cfg.MaxOpenConns = v
	}

	if v, err := positiveIntEnv(EnvMaxIdleConns); err != nil {
		return PoolConfig{}, err
	} else if v > 0 {
		cfg.MaxIdleConns = v
	}

	if v, err := positiveDurationEnv(EnvConnMaxLifetime); err != nil {
		return PoolConfig{}, err
	} else if v > 0 {
		cfg.ConnMaxLifetime = v
	}

	if v, err := positiveDurationEnv(EnvConnMaxIdleTime); err != nil {
		return PoolConfig{}, err
	} else if v > 0 {
		cfg.ConnMaxIdleTime = v
	}

	// database/sql silently caps idle to open under the hood, but it is
	// less surprising — and makes the metric we emit later truthful — to
	// clamp here.
	if cfg.MaxIdleConns > cfg.MaxOpenConns {
		cfg.MaxIdleConns = cfg.MaxOpenConns
	}

	return cfg, nil
}

func positiveIntEnv(name string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q is not an integer", errInvalidPoolEnv, name, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %s=%d must be > 0", errInvalidPoolEnv, name, n)
	}
	return n, nil
}

func positiveDurationEnv(name string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q is not a duration: %v", errInvalidPoolEnv, name, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%w: %s=%v must be > 0", errInvalidPoolEnv, name, d)
	}
	return d, nil
}
