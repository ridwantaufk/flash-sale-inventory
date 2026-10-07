package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Options struct {
	MaxConns         int
	StatementTimeout time.Duration
}

func Open(ctx context.Context, url string, o Options) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	milliSecond := strconv.FormatInt(o.StatementTimeout.Milliseconds(), 10)
	cfg.RuntimeParams["statement_timeout"] = milliSecond
	cfg.RuntimeParams["idle_in_transaction_session_timeout"] = milliSecond

	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(o.MaxConns)
	db.SetMaxIdleConns(o.MaxConns)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return db, nil
}
