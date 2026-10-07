package inventory

import (
	"context"
	"testing"
	"time"
)

// sweepRecorder lets the reaper report its own cadence without a database and
// without the test goroutine racing the reaper goroutine over a counter.
type sweepRecorder struct {
	batches chan int
}

func (s *sweepRecorder) Reserve(context.Context, string, string, int, time.Duration) (Reservation, error) {
	return Reservation{}, nil
}

func (s *sweepRecorder) Confirm(context.Context, string) (Reservation, error) {
	return Reservation{}, nil
}

func (s *sweepRecorder) Stock(context.Context, string) (Stock, error) {
	return Stock{}, nil
}

func (s *sweepRecorder) ExpireDue(ctx context.Context, batch int) (int, error) {
	select {
	case s.batches <- batch:
	default:
	}
	return 0, ctx.Err()
}

func (s *sweepRecorder) Drifted(context.Context) ([]string, error) {
	return nil, nil
}

func TestReaperSweepsImmediatelyThenOnItsInterval(t *testing.T) {
	recorder := &sweepRecorder{batches: make(chan int, 8)}
	svc := NewService(recorder, ServiceOptions{ReaperBatch: 40, WriteTimeout: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		NewReaper(svc, 15*time.Millisecond, quietLog()).Run(ctx)
	}()

	// The opening pass is what keeps a reservation that expired during a
	// restart from waiting a whole interval for its first sweep.
	select {
	case batch := <-recorder.batches:
		if batch != 40 {
			t.Errorf("sweep used batch %d, want the configured 40", batch)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reaper never swept")
	}

	intervalSweeps := 0
	cutoff := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case <-recorder.batches:
			intervalSweeps++
			if intervalSweeps >= 3 {
				break loop
			}
		case <-cutoff:
			t.Fatalf("only %d further sweeps in 200ms at a 15ms interval", intervalSweeps)
		}
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("reaper kept running after cancellation")
	}
}
