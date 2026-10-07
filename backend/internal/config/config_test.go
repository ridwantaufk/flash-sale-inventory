package config

import (
	"testing"
	"time"
)

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_ADDR", "DATABASE_URL", "GIN_MODE", "RESERVATION_TTL", "REAPER_INTERVAL",
		"REAPER_BATCH", "STATEMENT_TIMEOUT", "HANDLER_TIMEOUT", "SHUTDOWN_TIMEOUT", "MAX_CONNS",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://flashsale:flashsale@localhost:5433/flashsale?sslmode=disable")
}

func TestLoadRequiresADatabaseURL(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an empty DATABASE_URL")
	}
}

func TestShutdownBudgetMustOutlastTheStatementTimeout(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("STATEMENT_TIMEOUT", "5s")
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")

	if _, err := Load(); err == nil {
		t.Fatal("an equal budget was accepted; nothing is left to drain")
	}

	t.Setenv("SHUTDOWN_TIMEOUT", "15s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("a larger budget was rejected: %v", err)
	}
	if cfg.ShutdownTimeout <= cfg.StatementTimeout {
		t.Errorf("shutdown %s <= statement %s", cfg.ShutdownTimeout, cfg.StatementTimeout)
	}
}

func TestUnparseableDurationsFallBackInsteadOfServingZero(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("RESERVATION_TTL", "five minutes")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReservationTTL != 5*time.Minute {
		t.Errorf("ttl = %s, want the default", cfg.ReservationTTL)
	}
}

func TestNegativeIntervalsAreRejected(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("RESERVATION_TTL", "-1s")
	if _, err := Load(); err == nil {
		t.Error("a negative reservation ttl was accepted")
	}

	t.Setenv("RESERVATION_TTL", "5m")
	t.Setenv("REAPER_INTERVAL", "0s")
	if _, err := Load(); err == nil {
		t.Error("a zero reaper interval was accepted")
	}
}

func TestDefaultsMatchTheSpec(t *testing.T) {
	clearEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReservationTTL != 5*time.Minute {
		t.Errorf("ttl = %s, want 5m as the assignment asks for", cfg.ReservationTTL)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GinMode != "release" {
		t.Errorf("addr = %q, mode = %q", cfg.HTTPAddr, cfg.GinMode)
	}
}

func TestEnvIntRejectsTrailingGarbage(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("REAPER_BATCH", "500abc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReaperBatch != 500 {
		t.Errorf("batch = %d, want the default rather than a prefix of the garbage", cfg.ReaperBatch)
	}
}

func TestEnvIntAcceptsARealNumber(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("REAPER_BATCH", "120")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ReaperBatch != 120 {
		t.Errorf("batch = %d, want 120", cfg.ReaperBatch)
	}
}
