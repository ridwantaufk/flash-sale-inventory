package inventory

import (
	"context"
	"log/slog"
	"time"
)

type Reaper struct {
	svc      *Service
	interval time.Duration
	log      *slog.Logger
}

func NewReaper(svc *Service, interval time.Duration, log *slog.Logger) *Reaper {
	return &Reaper{svc: svc, interval: interval, log: log}
}

func (r *Reaper) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.pass(ctx)
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			r.log.Info("expiry reaper stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			r.pass(ctx)
			if tick%12 == 0 {
				r.reconcile(ctx)
			}
		}
	}
}

// Passes detach from cancellation so a sweep that started just before a
// shutdown signal finishes instead of rolling back and delaying the next pod.
func (r *Reaper) pass(ctx context.Context) {
	passCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*r.interval)
	defer cancel()

	expired, err := r.svc.ExpireDueReservations(passCtx)
	if err != nil {
		r.log.Error("expiry pass failed", "error", err)
		return
	}
	if expired > 0 {
		r.log.Info("expired reservations returned to stock", "count", expired)
	}
}

// Drift is a finding, not something to overwrite: reserved_stock is derived, so
// a mismatch means a write path is wrong somewhere.
func (r *Reaper) reconcile(ctx context.Context) {
	drifted, err := r.svc.Reconcile(ctx)
	if err != nil {
		r.log.Error("reconciliation failed", "error", err)
		return
	}
	if len(drifted) > 0 {
		r.log.Error("reserved_stock drifted from live reservations", "items", drifted)
	}
}
