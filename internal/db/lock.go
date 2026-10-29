package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Advisory lock keys. They are arbitrary but fixed forever: changing one
// would let an old and a new binary both believe they hold the lock.
const (
	LockMigrate   int64 = 0x4d495354_00000001 // "MIST" + 1
	LockScheduler int64 = 0x4d495354_00000002
)

// Leader is a session advisory lock held on a dedicated connection. Postgres
// releases it when that connection ends, so losing the connection means
// losing leadership; Lost reports that.
//
// The lock alone cannot fence a request that was already sent to an
// executor before leadership was lost. Callers persist operation intent
// before external calls, and a new leader reconciles those intents first.
type Leader struct {
	conn *pgx.Conn
	lost chan struct{}
	err  error
	stop context.CancelFunc
	done chan struct{}
	once sync.Once
}

// AcquireLeader opens a connection and blocks until it holds the lock with
// the given key, or ctx ends. Every check interval it confirms the
// connection still works and the lock is still held.
func AcquireLeader(ctx context.Context, url string, key int64, check time.Duration) (*Leader, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("db: leader connect: %w", err)
	}
	// pg_advisory_lock waits server-side; pgx cancels it if ctx ends.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		conn.Close(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("db: acquire lock: %w", err)
	}
	watchCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	l := &Leader{conn: conn, lost: make(chan struct{}), stop: stop, done: make(chan struct{})}
	go l.watch(watchCtx, key, check)
	return l, nil
}

func (l *Leader) watch(ctx context.Context, key int64, check time.Duration) {
	defer close(l.done)
	t := time.NewTicker(check)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		probe, cancel := context.WithTimeout(ctx, check)
		var held bool
		err := l.conn.QueryRow(probe, `
			SELECT count(*) > 0 FROM pg_locks
			WHERE locktype = 'advisory' AND granted AND pid = pg_backend_pid()
			  AND classid = ($1::bigint >> 32)::oid AND objid = ($1::bigint & x'ffffffff'::bigint)::oid
			  AND objsubid = 1`, key).Scan(&held)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil && !held {
			err = errors.New("advisory lock no longer held")
		}
		if err != nil {
			l.err = fmt.Errorf("db: leadership lost: %w", err)
			close(l.lost)
			return
		}
	}
}

// Lost is closed when the connection or lock is gone. Stop dispatch at once.
func (l *Leader) Lost() <-chan struct{} { return l.lost }

// Err explains why Lost closed.
func (l *Leader) Err() error {
	select {
	case <-l.lost:
		return l.err
	default:
		return nil
	}
}

// Release gives up the lock by closing the connection.
func (l *Leader) Release(ctx context.Context) {
	l.once.Do(func() {
		l.stop()
		<-l.done
		l.conn.Close(ctx)
	})
}
