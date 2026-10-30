// Package api is the HTTP API that `mistd api` serves: conventions,
// authentication middleware and the job endpoints. The contract is
// docs/agents/architecture/api.md. Identity, storage and admin endpoints
// are added by their owners on the same Server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor/k3s"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/scheduler"
)

// LogSource gives live output of a job, or the stored tail once the job
// has ended. executor.Executor satisfies it.
type LogSource interface {
	Logs(ctx context.Context, jobID int64, follow bool) (io.ReadCloser, error)
}

// CleanupRecorder accepts node cleanup receipts. *k3s.Executor satisfies it.
type CleanupRecorder interface {
	RecordCleanup(ctx context.Context, r k3s.CleanupReceipt) error
}

// Server holds the API's dependencies. Nil optional fields make their
// endpoints answer `unavailable` rather than guess.
type Server struct {
	Jobs  scheduler.Jobs
	Authz identity.Authorizer
	// Accounts resolve /v1 callers (portal session, relay, API token), in
	// order. Hosts resolves /internal callers. Neither accepts the other's
	// credentials.
	Accounts []Authenticator
	Hosts    Authenticator
	Logs     LogSource       // optional
	Cleanup  CleanupRecorder // optional
	Clock    clock.Clock
	Log      *slog.Logger
	// AllowedOrigins are the browser origins allowed to open WebSockets.
	AllowedOrigins []string
	// StreamRecheck is how often an open stream re-checks authorization
	// (default 10 s).
	StreamRecheck time.Duration
	// Ping checks the database for /healthz.
	Ping func(context.Context) error
}

// Handler returns the API's routes.
func (s *Server) Handler() http.Handler {
	if s.Clock == nil {
		s.Clock = clock.Real{}
	}
	if s.Log == nil {
		s.Log = slog.New(slog.DiscardHandler)
	}
	if s.StreamRecheck == 0 {
		s.StreamRecheck = 10 * time.Second
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.Handle("POST /v1/jobs", s.account(s.submit))
	mux.Handle("GET /v1/jobs", s.account(s.listJobs))
	mux.Handle("GET /v1/jobs/{id}", s.account(s.getJob))
	mux.Handle("POST /v1/jobs/{id}/cancel", s.account(s.cancelJob))
	mux.Handle("GET /v1/jobs/{id}/log", s.account(s.jobLog))
	mux.Handle("GET /v1/partitions", s.account(s.partitions))
	mux.Handle("GET /v1/queue", s.account(s.queue))
	mux.Handle("POST /internal/v1/executions/{execution_id}/cleanup", s.host(s.cleanup))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &Error{http.StatusNotFound, CodeNotFound, "no such endpoint", nil})
	})
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if s.Ping != nil {
		if err := s.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	io.WriteString(w, "ok\n")
}

// Error codes (api.md#conventions).
const (
	CodeUnauthenticated = "unauthenticated"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeInvalid         = "invalid"
	CodeLimitExceeded   = "limit_exceeded"
	CodeConflict        = "conflict"
	CodeUnavailable     = "unavailable"
	CodeInternal        = "internal"
)

// Error is an API error reply.
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func invalid(field, msg string) *Error {
	return &Error{http.StatusBadRequest, CodeInvalid, msg, map[string]any{"field": field}}
}

func limitExceeded(field, msg string) *Error {
	return &Error{http.StatusUnprocessableEntity, CodeLimitExceeded, msg, map[string]any{"field": field}}
}

func notFound(msg string) *Error { return &Error{http.StatusNotFound, CodeNotFound, msg, nil} }

func unavailable(msg string) *Error {
	return &Error{http.StatusServiceUnavailable, CodeUnavailable, msg, nil}
}

func writeError(w http.ResponseWriter, e *Error) {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	writeJSON(w, e.Status, map[string]*Error{"error": e})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// fail writes err, which is an *Error or an unexpected failure. Internal
// details are logged, never returned.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *Error
	if errors.As(err, &e) {
		writeError(w, e)
		return
	}
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		return
	}
	s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, &Error{http.StatusInternalServerError, CodeInternal, "internal error", nil})
}

// authzError turns an Authorizer error into a reply. With hide, refusals
// become not_found, so non-members learn nothing about a job.
func authzError(err error, hide bool, what string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnavailable):
		return unavailable("cannot confirm current account and team membership; try again shortly")
	case errors.Is(err, identity.ErrForbidden), errors.Is(err, identity.ErrRevoked):
		if hide {
			return notFound(what + " not found")
		}
		return &Error{http.StatusForbidden, CodeForbidden, "not allowed", nil}
	}
	return err
}
