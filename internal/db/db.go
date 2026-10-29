// Package db owns the Postgres connection, embedded migrations and the
// advisory locks that serialize migrations and scheduler leadership.
//
// Postgres is the source of truth (D3). Each owner keeps its schema in
// migrations/<owner>/ and accesses its tables only through its own package.
//
// # Lock order
//
// Transactions that touch more than one owner's rows take row locks in this
// order, and never in reverse, so that admission, cancellation and revocation
// cannot deadlock each other:
//
//  1. account (identity)
//  2. membership (identity)
//  3. job (scheduler)
//  4. allocation (scheduler)
//  5. node (scheduler)
//  6. execution receipt (executor)
//
// Identity's admission methods take locks 1–2 inside the caller's
// transaction; the scheduler then takes 3–5.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open connects a pool and checks that the server answers.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("db: parse config: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	return pool, nil
}
