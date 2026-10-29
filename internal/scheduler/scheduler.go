// Package scheduler owns the queue, policy, placement and accounting. Its
// contract is docs/agents/architecture/scheduler.md.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
)

type Config struct {
	DatabaseURL string
	Pool        *pgxpool.Pool
	Clock       clock.Clock
	Log         *slog.Logger

	// Tick is the cycle period when nothing triggers one sooner.
	Tick time.Duration
	// LeaderCheck is how often leadership is confirmed.
	LeaderCheck time.Duration
}

// Run waits for the scheduler lock, then runs cycles until ctx ends or
// leadership is lost. Losing leadership returns an error at once, so that
// nothing else is dispatched; the process should exit and restart.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Tick == 0 {
		cfg.Tick = 10 * time.Second
	}
	if cfg.LeaderCheck == 0 {
		cfg.LeaderCheck = 2 * time.Second
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}

	cfg.Log.Info("waiting for scheduler lock")
	leader, err := db.AcquireLeader(ctx, cfg.DatabaseURL, db.LockScheduler, cfg.LeaderCheck)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer leader.Release(context.WithoutCancel(ctx))
	cfg.Log.Info("holding scheduler lock")

	for {
		// The scheduling cycle (M1 step 3) goes here: reconcile intents,
		// then admit. It must check leader.Lost() between external calls.
		t := cfg.Clock.NewTimer(cfg.Tick)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-leader.Lost():
			t.Stop()
			return leader.Err()
		case <-t.C():
		}
	}
}
