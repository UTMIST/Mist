// Package scheduler owns the queue, policy, placement and accounting. Its
// contract is docs/agents/architecture/scheduler.md.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
)

type Config struct {
	DatabaseURL string
	Pool        *pgxpool.Pool
	Identity    identity.Admitter
	// Executors by name, matching partitions.executor and nodes.executor.
	Executors map[string]executor.Executor
	Clock     clock.Clock
	Log       *slog.Logger

	// Tick is the cycle period when nothing triggers one sooner.
	Tick time.Duration
	// LeaderCheck is how often leadership is confirmed.
	LeaderCheck time.Duration
	// LaunchTimeout is admission to work start, including image pulls.
	LaunchTimeout time.Duration
	// CallTimeout bounds each executor call.
	CallTimeout time.Duration
}

// Scheduler is the active leader's state. Create it with New after
// acquiring leadership; tests drive Cycle and HandleEvent directly.
type Scheduler struct {
	pool          *pgxpool.Pool
	identity      identity.Admitter
	executors     map[string]executor.Executor
	clock         clock.Clock
	log           *slog.Logger
	epoch         int64
	launchTimeout time.Duration
	callTimeout   time.Duration
	stopRetry     time.Duration
	lost          <-chan struct{}

	// Per-cycle observations, rebuilt every cycle.
	observed  map[string]bool   // executor -> Nodes and List complete
	nodeExec  map[string]string // node -> executor
	unknownOn map[string]bool   // node hosts an execution Mist did not admit
	missing   map[int64]int     // job -> consecutive complete lists without it
}

// New takes leadership: it bumps the epoch, which fences every older
// scheduler's writes. lost, if not nil, ends dispatch when closed.
func New(ctx context.Context, cfg Config, lost <-chan struct{}) (*Scheduler, error) {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.LaunchTimeout == 0 {
		cfg.LaunchTimeout = 10 * time.Minute
	}
	if cfg.CallTimeout == 0 {
		cfg.CallTimeout = 30 * time.Second
	}
	epoch, err := takeLeadership(ctx, cfg.Pool)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		pool:          cfg.Pool,
		identity:      cfg.Identity,
		executors:     cfg.Executors,
		clock:         cfg.Clock,
		log:           cfg.Log,
		epoch:         epoch,
		launchTimeout: cfg.LaunchTimeout,
		callTimeout:   cfg.CallTimeout,
		stopRetry:     30 * time.Second,
		lost:          lost,
		observed:      map[string]bool{},
		nodeExec:      map[string]string{},
		unknownOn:     map[string]bool{},
		missing:       map[int64]int{},
	}, nil
}

func (s *Scheduler) leaderCheck() error {
	if s.lost == nil {
		return nil
	}
	select {
	case <-s.lost:
		return ErrNotLeader
	default:
		return nil
	}
}

// Cycle runs one scheduling pass. Reconciliation comes first, so a new
// leader resolves outstanding operations before admitting anything.
func (s *Scheduler) Cycle(ctx context.Context) error {
	for _, step := range []func(context.Context) error{
		s.refreshNodes,
		s.applyRevocations,
		s.reconcile,
		s.enforceDeadlines,
		s.admit,
		s.dispatch,
	} {
		if err := s.leaderCheck(); err != nil {
			return err
		}
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
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

	s, err := New(ctx, cfg, leader.Lost())
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wake := make(chan struct{}, 1)
	trigger := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	go listen(runCtx, cfg.DatabaseURL, trigger, cfg.Clock, cfg.Log)
	events := make(chan executor.Event)
	for _, ex := range cfg.Executors {
		go func() {
			for {
				select {
				case ev := <-ex.Events():
					select {
					case events <- ev:
					case <-runCtx.Done():
						return
					}
				case <-runCtx.Done():
					return
				}
			}
		}()
	}

	trigger()
	for {
		t := cfg.Clock.NewTimer(cfg.Tick)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-leader.Lost():
			t.Stop()
			return leader.Err()
		case ev := <-events:
			t.Stop()
			if err := s.HandleEvent(ctx, ev); err != nil {
				if errors.Is(err, ErrNotLeader) {
					return err
				}
				cfg.Log.Error("handle event", "event", ev.EventID, "err", err)
			}
			trigger()
			continue
		case <-wake:
		case <-t.C():
		}
		t.Stop()
		if err := s.Cycle(ctx); err != nil {
			if errors.Is(err, ErrNotLeader) {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			cfg.Log.Error("scheduling cycle", "err", err)
		}
	}
}

// listen wakes the leader when the API submits or cancels, through
// Postgres NOTIFY. It is only a latency optimization: the tick still runs.
func listen(ctx context.Context, url string, trigger func(), clk clock.Clock, log *slog.Logger) {
	for ctx.Err() == nil {
		conn, err := pgx.Connect(ctx, url)
		if err == nil {
			_, err = conn.Exec(ctx, "LISTEN "+notifyChannel)
			for err == nil {
				_, err = conn.WaitForNotification(ctx)
				if err == nil {
					trigger()
				}
			}
			conn.Close(context.WithoutCancel(ctx))
		}
		if ctx.Err() != nil {
			return
		}
		log.Warn("scheduler notify listener reconnecting", "err", err)
		t := clk.NewTimer(time.Second)
		select {
		case <-t.C():
		case <-ctx.Done():
			t.Stop()
		}
	}
}
