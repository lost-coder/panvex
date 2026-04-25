package postgres

import (
	"errors"
	"testing"
	"time"
)

func TestLoadPoolConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv(EnvMaxOpenConns, "")
	t.Setenv(EnvMaxIdleConns, "")
	t.Setenv(EnvConnMaxLifetime, "")
	t.Setenv(EnvConnMaxIdleTime, "")

	cfg, err := loadPoolConfigFromEnv()
	if err != nil {
		t.Fatalf("loadPoolConfigFromEnv: %v", err)
	}
	if cfg.MaxOpenConns != defaultMaxOpenConns {
		t.Errorf("MaxOpenConns = %d, want %d", cfg.MaxOpenConns, defaultMaxOpenConns)
	}
	if cfg.MaxIdleConns != defaultMaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", cfg.MaxIdleConns, defaultMaxIdleConns)
	}
	if cfg.ConnMaxLifetime != defaultConnMaxLifetime {
		t.Errorf("ConnMaxLifetime = %v, want %v", cfg.ConnMaxLifetime, defaultConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != defaultConnMaxIdleTime {
		t.Errorf("ConnMaxIdleTime = %v, want %v", cfg.ConnMaxIdleTime, defaultConnMaxIdleTime)
	}
}

func TestLoadPoolConfigFromEnv_Overrides(t *testing.T) {
	t.Setenv(EnvMaxOpenConns, "100")
	t.Setenv(EnvMaxIdleConns, "30")
	t.Setenv(EnvConnMaxLifetime, "45m")
	t.Setenv(EnvConnMaxIdleTime, "15m")

	cfg, err := loadPoolConfigFromEnv()
	if err != nil {
		t.Fatalf("loadPoolConfigFromEnv: %v", err)
	}
	if cfg.MaxOpenConns != 100 {
		t.Errorf("MaxOpenConns = %d, want 100", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 30 {
		t.Errorf("MaxIdleConns = %d, want 30", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 45*time.Minute {
		t.Errorf("ConnMaxLifetime = %v, want 45m", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 15*time.Minute {
		t.Errorf("ConnMaxIdleTime = %v, want 15m", cfg.ConnMaxIdleTime)
	}
}

func TestLoadPoolConfigFromEnv_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		env  string
		val  string
	}{
		{"non-numeric max open conns", EnvMaxOpenConns, "abc"},
		{"negative max open conns", EnvMaxOpenConns, "-1"},
		{"zero max open conns", EnvMaxOpenConns, "0"},
		{"non-numeric max idle conns", EnvMaxIdleConns, "xyz"},
		{"negative max idle conns", EnvMaxIdleConns, "-5"},
		{"unparseable lifetime", EnvConnMaxLifetime, "not-a-duration"},
		{"negative lifetime", EnvConnMaxLifetime, "-5m"},
		{"unparseable idle time", EnvConnMaxIdleTime, "garbage"},
		{"negative idle time", EnvConnMaxIdleTime, "-1h"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset the others to a known-good empty so a single bad value is the
			// only thing under test.
			t.Setenv(EnvMaxOpenConns, "")
			t.Setenv(EnvMaxIdleConns, "")
			t.Setenv(EnvConnMaxLifetime, "")
			t.Setenv(EnvConnMaxIdleTime, "")
			t.Setenv(tc.env, tc.val)

			if _, err := loadPoolConfigFromEnv(); err == nil {
				t.Fatalf("expected error for %s=%q, got nil", tc.env, tc.val)
			} else if !errors.Is(err, errInvalidPoolEnv) {
				t.Fatalf("expected errInvalidPoolEnv, got %v", err)
			}
		})
	}
}

func TestLoadPoolConfigFromEnv_IdleExceedsOpenIsClamped(t *testing.T) {
	// database/sql panics if MaxIdleConns > MaxOpenConns; we coerce silently.
	t.Setenv(EnvMaxOpenConns, "10")
	t.Setenv(EnvMaxIdleConns, "50")
	t.Setenv(EnvConnMaxLifetime, "")
	t.Setenv(EnvConnMaxIdleTime, "")

	cfg, err := loadPoolConfigFromEnv()
	if err != nil {
		t.Fatalf("loadPoolConfigFromEnv: %v", err)
	}
	if cfg.MaxIdleConns > cfg.MaxOpenConns {
		t.Errorf("MaxIdleConns (%d) must not exceed MaxOpenConns (%d)",
			cfg.MaxIdleConns, cfg.MaxOpenConns)
	}
}
