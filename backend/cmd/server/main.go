package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ridwantaufk/flash-sale-inventory/internal/config"
	"github.com/ridwantaufk/flash-sale-inventory/internal/inventory"
	"github.com/ridwantaufk/flash-sale-inventory/internal/platform/pg"
	"github.com/ridwantaufk/flash-sale-inventory/internal/server"
	"github.com/ridwantaufk/flash-sale-inventory/internal/transport/httpx"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pg.Open(ctx, cfg.DatabaseURL, pg.Options{MaxConns: cfg.MaxConns, StatementTimeout: cfg.StatementTimeout})
	if err != nil {
		return err
	}
	defer db.Close()

	repo := inventory.NewPostgresRepository(db, log)
	svc := inventory.NewService(repo, inventory.ServiceOptions{
		ReservationTTL: cfg.ReservationTTL,
		ReaperBatch:    cfg.ReaperBatch,
		WriteTimeout:   cfg.HandlerTimeout,
	})
	reaper := inventory.NewReaper(svc, cfg.ReaperInterval, log)

	ready := httpx.NewReadiness()
	engine := server.NewEngine(cfg, inventory.NewHandler(svc, log), ready, server.Pinger(db))
	httpSrv := server.NewHTTPServer(cfg, engine)

	reaperCtx, cancelReaper := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reaper.Run(reaperCtx)
	}()

	serveErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr,
			"reservation_ttl", cfg.ReservationTTL.String(),
			"reaper_interval", cfg.ReaperInterval.String())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("listen %s: %w", cfg.HTTPAddr, err)
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		cancelReaper()
		wg.Wait()
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received", "budget", cfg.ShutdownTimeout.String())
	}

	ready.Set(false)

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown did not drain in time", "error", err)
		if cerr := httpSrv.Close(); cerr != nil {
			log.Error("forced close failed", "error", cerr)
		}
	}

	cancelReaper()
	wg.Wait()

	log.Info("shutdown complete")
	return nil
}
