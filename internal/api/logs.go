package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/scheduler"
)

// tailBytes is how much output GET /v1/jobs/{id}/log returns.
const tailBytes = 64 << 10

// Log is the reply of GET /v1/jobs/{id}/log. The complete output is the
// file at OutputPath; the API never reads user paths itself.
type Log struct {
	State            string `json:"state"`
	OutputPath       string `json:"output_path"`
	OutputIncomplete bool   `json:"output_incomplete"`
	Truncated        bool   `json:"truncated"` // only the last 64 KiB is shown
	Text             string `json:"text"`
}

// jobLog handles GET /v1/jobs/{id}/log, and ?follow=1 over a WebSocket.
func (s *Server) jobLog(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	id, err := jobID(r)
	if err != nil {
		return err
	}
	j, err := s.viewableJob(r.Context(), p, id)
	if err != nil {
		return err
	}
	if s.Logs == nil {
		return unavailable("job output is not available from this API server")
	}
	if r.URL.Query().Get("follow") == "1" {
		return s.followLog(w, r, p, j)
	}
	out := Log{State: string(j.State), OutputPath: j.OutputPath, OutputIncomplete: j.OutputIncomplete}
	if j.Node != nil { // never admitted: no output anywhere yet
		rc, err := s.Logs.Logs(r.Context(), id, false)
		switch {
		case errors.Is(err, executor.ErrNotFound):
		case err != nil:
			return err
		default:
			defer rc.Close()
			text, truncated, err := lastBytes(rc, tailBytes)
			if err != nil {
				return err
			}
			out.Text, out.Truncated = text, truncated
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// lastBytes reads r to the end, keeping only the last n bytes.
func lastBytes(r io.Reader, n int) (string, bool, error) {
	buf := make([]byte, 0, 2*n)
	chunk := make([]byte, 32<<10)
	truncated := false
	for {
		k, err := r.Read(chunk)
		buf = append(buf, chunk[:k]...)
		if len(buf) > n {
			buf = append(buf[:0], buf[len(buf)-n:]...)
			truncated = true
		}
		if errors.Is(err, io.EOF) {
			return strings.ToValidUTF8(string(buf), "�"), truncated, nil
		}
		if err != nil {
			return "", false, err
		}
	}
}

// followLog streams output as binary WebSocket frames until the job's
// output ends, the client goes away or the caller loses access, which is
// re-checked every StreamRecheck.
func (s *Server) followLog(w http.ResponseWriter, r *http.Request, p identity.Principal, j *scheduler.JobInfo) error {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return invalid("follow", "follow=1 needs a WebSocket upgrade")
	}
	if !s.originAllowed(r.Header.Get("Origin")) {
		return &Error{http.StatusForbidden, CodeForbidden, "origin not allowed", nil}
	}
	ws := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil }, // origin checked above
		Handler: func(conn *websocket.Conn) {
			defer conn.Close()
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			go func() { // a client frame or a closed connection ends the stream
				io.Copy(io.Discard, conn)
				cancel()
			}()
			go func() {
				t := time.NewTicker(s.StreamRecheck)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						if err := s.Authz.Authorize(ctx, p, identity.ActionViewJob, target(j)); err != nil {
							cancel()
							return
						}
					}
				}
			}()
			if j.Node == nil {
				return // not admitted: nothing to follow yet; the client retries
			}
			rc, err := s.Logs.Logs(ctx, j.ID, true)
			if err != nil {
				return
			}
			go func() { <-ctx.Done(); rc.Close() }()
			buf := make([]byte, 32<<10)
			for {
				n, err := rc.Read(buf)
				if n > 0 {
					if websocket.Message.Send(conn, buf[:n]) != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		},
	}
	ws.ServeHTTP(w, r)
	return nil
}

// originAllowed: requests without an Origin header are not from a browser
// and are authenticated by other means; browsers must be allowlisted.
func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return slices.Contains(s.AllowedOrigins, u.Scheme+"://"+u.Host)
}
