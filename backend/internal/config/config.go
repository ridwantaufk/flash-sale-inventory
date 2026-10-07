package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr         string
	DatabaseURL      string
	GinMode          string
	ReservationTTL   time.Duration
	ReaperInterval   time.Duration
	ReaperBatch      int
	StatementTimeout time.Duration
	HandlerTimeout   time.Duration
	ShutdownTimeout  time.Duration
	MaxConns         int
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:         envString("HTTP_ADDR", ":8080"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		GinMode:          envString("GIN_MODE", "release"),
		ReservationTTL:   envDuration("RESERVATION_TTL", 5*time.Minute),
		ReaperInterval:   envDuration("REAPER_INTERVAL", 5*time.Second),
		ReaperBatch:      envInt("REAPER_BATCH", 500),
		StatementTimeout: envDuration("STATEMENT_TIMEOUT", 5*time.Second),
		HandlerTimeout:   envDuration("HANDLER_TIMEOUT", 10*time.Second),
		ShutdownTimeout:  envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		MaxConns:         envInt("MAX_CONNS", 25),
	}

	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.ReservationTTL <= 0 {
		return c, fmt.Errorf("RESERVATION_TTL must be positive, got %s", c.ReservationTTL)
	}
	if c.ReaperInterval <= 0 {
		return c, fmt.Errorf("REAPER_INTERVAL must be positive, got %s", c.ReaperInterval)
	}
	/* Draining must outlast the statement it is waiting for, otherwise Shutdown
	// returns while the transaction is alive and the pool closes under it. */
	if c.ShutdownTimeout <= c.StatementTimeout {
		return c, fmt.Errorf("SHUTDOWN_TIMEOUT (%s) must exceed STATEMENT_TIMEOUT (%s)",
			c.ShutdownTimeout, c.StatementTimeout)
	}
	return c, nil
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
