package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/executor/k3s"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/identity/identitytest"
	"github.com/UTMIST/Mist/internal/scheduler"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// testUser stands in for Identity's session and relay authenticators.
type testUser struct{}

func (testUser) Authenticate(r *http.Request) (identity.Principal, error) {
	u := r.Header.Get("X-Test-User")
	if u == "" {
		return identity.Principal{}, ErrNoCredentials
	}
	return identity.Principal{Kind: identity.PrincipalAccount, Username: u}, nil
}

type fakeLogs struct {
	mu   sync.Mutex
	text map[int64]string
}

func (f *fakeLogs) Logs(_ context.Context, id int64, _ bool) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.text[id]
	if !ok {
		return nil, executor.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(t)), nil
}

type fakeCleanup struct {
	mu   sync.Mutex
	got  []k3s.CleanupReceipt
	fail error
}

func (f *fakeCleanup) RecordCleanup(_ context.Context, r k3s.CleanupReceipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, r)
	return nil
}

type env struct {
	t       *testing.T
	ctx     context.Context
	srv     *httptest.Server
	s       *Server
	id      *identitytest.Fixture
	logs    *fakeLogs
	cleanup *fakeCleanup
	clock   *clock.Fake
}

func newEnv(t *testing.T) *env {
	ctx := context.Background()
	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO nodes (name, executor, healthy, observed_at, milli_cpu, memory_bytes, gpus, scratch_bytes)
		 VALUES ('utmist-z1opa08', 'k3s', true, now(), 32000, 137438953472, 2, 429496729600),
		        ('utmist-tt', 'k3s', true, now(), 32000, 274877906944, 0, 1099511627776)`,
		"INSERT INTO partition_nodes VALUES ('gpu', 'utmist-z1opa08'), ('cpu', 'utmist-tt')",
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tokens := filepath.Join(t.TempDir(), "tokens")
	os.WriteFile(tokens, []byte("# host scope hash\n"+
		"utmist-tt cleanup "+HashToken("tt-secret")+"\n"+
		"utmist-tt provision "+HashToken("tt-provision")+"\n"), 0o600)
	hosts, err := LoadHostTokens(tokens, "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(t0)
	e := &env{t: t, ctx: ctx, id: identitytest.New(), clock: c,
		logs: &fakeLogs{text: map[int64]string{}}, cleanup: &fakeCleanup{}}
	e.s = &Server{
		Jobs: scheduler.Jobs{Pool: pool, Clock: c}, Authz: e.id,
		Accounts: []Authenticator{testUser{}}, Hosts: hosts,
		Logs: e.logs, Cleanup: e.cleanup, Clock: c,
		AllowedOrigins: []string{"https://mist.example"},
		StreamRecheck:  20 * time.Millisecond,
	}
	e.srv = httptest.NewServer(e.s.Handler())
	t.Cleanup(e.srv.Close)
	return e
}

type reply struct {
	status int
	header http.Header
	body   []byte
}

func (r reply) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r reply) code(t *testing.T) string {
	t.Helper()
	var e struct{ Error Error }
	r.decode(t, &e)
	return e.Error.Code
}

func (e *env) do(method, path, user string, body any, hdr ...string) reply {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, b}
}

var keys int

func (e *env) submit(user string, body any) reply {
	e.t.Helper()
	keys++
	return e.do("POST", "/v1/jobs", user, body, "Idempotency-Key", fmt.Sprintf("key-%d", keys))
}

func batch(team string) map[string]any {
	return map[string]any{"team": team, "kind": "batch", "script": "#!/bin/bash\necho hi\n"}
}

func (e *env) mustSubmit(user string, body any) Job {
	e.t.Helper()
	r := e.submit(user, body)
	if r.status != http.StatusCreated {
		e.t.Fatalf("submit: %d %s", r.status, r.body)
	}
	var j Job
	r.decode(e.t, &j)
	return j
}

func TestSubmitFillsDefaults(t *testing.T) {
	e := newEnv(t)
	j := e.mustSubmit("alice", map[string]any{"team": "team-a", "kind": "batch", "script": "x", "resources": map[string]any{"gpus": 2}})
	want := Resources{CPUs: 16, MemoryBytes: 64 << 30, GPUs: 2, ScratchBytes: 100 << 30}
	if j.State != "PENDING" || j.Partition != "gpu" || j.QOS != "normal" || j.Resources != want ||
		j.TimeLimitSeconds != 3600 || j.SignalGraceSeconds != 60 || j.Name != "batch" {
		t.Fatalf("defaults: %+v", j)
	}
	if j.Workdir != "/mist/teams/team-a/alice" || j.OutputPath != fmt.Sprintf("/mist/teams/team-a/alice/mist-%d.out", j.ID) {
		t.Fatalf("paths: %q %q", j.Workdir, j.OutputPath)
	}
	if !j.CanCancel || j.CanAttach {
		t.Fatalf("can_cancel/can_attach: %+v", j)
	}

	// Team derived from the workdir; CPU partition defaults are per job.
	j = e.mustSubmit("bob", map[string]any{"partition": "cpu", "kind": "command",
		"argv": []string{"/usr/bin/huggingface-cli", "download", "x"}, "workdir": "/mist/teams/team-a/bob/dl", "output": "logs/%j-%x.txt"})
	if j.Team != "team-a" || j.Resources.CPUs != 2 || j.Resources.MemoryBytes != 8<<30 || j.Name != "huggingface-cli" ||
		j.OutputPath != fmt.Sprintf("/mist/teams/team-a/bob/dl/logs/%d-%%x.txt", j.ID) {
		t.Fatalf("command job: %+v", j)
	}
}

// M1-09: a duplicate submission key gives one job; a conflicting payload
// under the same key is rejected; a retry survives a defaults change.
func TestSubmitIdempotency(t *testing.T) {
	e := newEnv(t)
	body := batch("team-a")
	first := e.do("POST", "/v1/jobs", "alice", body, "Idempotency-Key", "k1")
	if first.status != http.StatusCreated {
		t.Fatalf("%d %s", first.status, first.body)
	}
	var j1 Job
	first.decode(t, &j1)

	if _, err := e.s.Jobs.Pool.Exec(e.ctx, "UPDATE partitions SET default_time_limit_seconds = 7200"); err != nil {
		t.Fatal(err)
	}
	again := e.do("POST", "/v1/jobs", "alice", body, "Idempotency-Key", "k1")
	var j2 Job
	again.decode(t, &j2)
	if again.status != http.StatusOK || again.header.Get("Idempotent-Replayed") != "true" || j2.ID != j1.ID || j2.TimeLimitSeconds != 3600 {
		t.Fatalf("replay: %d %s", again.status, again.body)
	}

	changed := batch("team-a")
	changed["script"] = "#!/bin/bash\necho different\n"
	if r := e.do("POST", "/v1/jobs", "alice", changed, "Idempotency-Key", "k1"); r.status != http.StatusConflict || r.code(t) != CodeConflict {
		t.Fatalf("conflict: %d %s", r.status, r.body)
	}
	// Keys are per account: Bob's k1 is a different submission.
	if r := e.do("POST", "/v1/jobs", "bob", body, "Idempotency-Key", "k1"); r.status != http.StatusCreated {
		t.Fatalf("bob: %d %s", r.status, r.body)
	}
	if r := e.do("POST", "/v1/jobs", "alice", body); r.status != http.StatusBadRequest {
		t.Fatalf("missing key: %d", r.status)
	}

	// Concurrent retries of one key still give one job.
	var wg sync.WaitGroup
	ids := make(chan int64, 8)
	for range 8 {
		wg.Go(func() {
			r := e.do("POST", "/v1/jobs", "carol", batch("team-b"), "Idempotency-Key", "race")
			var j Job
			json.Unmarshal(r.body, &j)
			ids <- j.ID
		})
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 || seen[0] {
		t.Fatalf("concurrent retries made jobs %v", seen)
	}

	// The replay re-checks access: after leaving the team, Alice's retry
	// does not return the job.
	e.id.RemoveMember("alice", "team-a")
	if r := e.do("POST", "/v1/jobs", "alice", body, "Idempotency-Key", "k1"); r.status != http.StatusNotFound {
		t.Fatalf("replay after removal: %d %s", r.status, r.body)
	}
}

// M1-09: hostile paths, env and argv are rejected before they are stored.
func TestSubmitValidation(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name   string
		edit   func(map[string]any)
		status int
	}{
		{"dotdot workdir", func(b map[string]any) { b["workdir"] = "/mist/teams/team-a/../team-b/carol" }, 400},
		{"other team workdir", func(b map[string]any) { b["workdir"] = "/mist/teams/team-b/carol" }, 400},
		{"landing folder", func(b map[string]any) { b["workdir"] = "/mist/login/alice" }, 400},
		{"relative workdir", func(b map[string]any) { b["workdir"] = "team-a/alice" }, 400},
		{"trailing slash", func(b map[string]any) { b["workdir"] = "/mist/teams/team-a/alice/" }, 400},
		{"output escapes", func(b map[string]any) { b["output"] = "../../team-b/x.out" }, 400},
		{"output elsewhere", func(b map[string]any) { b["output"] = "/etc/cron.d/x" }, 400},
		{"output is team root", func(b map[string]any) { b["workdir"] = "/mist/teams/team-a"; b["output"] = "." }, 400},
		{"control char path", func(b map[string]any) { b["output"] = "a\nb.out" }, 400},
		{"protected env", func(b map[string]any) { b["env"] = map[string]string{"HOME": "/tmp"} }, 400},
		{"mist env", func(b map[string]any) { b["env"] = map[string]string{"MIST_JOB_ID": "1"} }, 400},
		{"device env", func(b map[string]any) { b["env"] = map[string]string{"CUDA_VISIBLE_DEVICES": "0,1"} }, 400},
		{"bad env name", func(b map[string]any) { b["env"] = map[string]string{"A=B": "c"} }, 400},
		{"nul env", func(b map[string]any) { b["env"] = map[string]string{"A": "b\x00c"} }, 400},
		{"nul arg", func(b map[string]any) { b["args"] = []string{"a\x00b"} }, 400},
		{"batch with argv", func(b map[string]any) { b["argv"] = []string{"ls"} }, 400},
		{"command with script", func(b map[string]any) { b["kind"] = "command"; b["argv"] = []string{"ls"} }, 400},
		{"interactive", func(b map[string]any) { b["kind"] = "interactive"; delete(b, "script") }, 400},
		{"interactive qos", func(b map[string]any) { b["qos"] = "interactive" }, 400},
		{"unknown field", func(b map[string]any) { b["uid"] = 0 }, 400},
		{"huge script", func(b map[string]any) { b["script"] = strings.Repeat("x", maxScript+1) }, 400},
		{"long name", func(b map[string]any) { b["name"] = strings.Repeat("n", 129) }, 400},
		{"fractional cpus", func(b map[string]any) { b["resources"] = map[string]any{"cpus": 1.5} }, 400},
		{"zero gpus on gpu", func(b map[string]any) { b["resources"] = map[string]any{"gpus": 0} }, 400},
		{"gpu on cpu", func(b map[string]any) { b["partition"] = "cpu"; b["resources"] = map[string]any{"gpus": 1} }, 400},
		{"grace too long", func(b map[string]any) { b["signal_grace_seconds"] = 121 }, 400},
		{"other runtime", func(b map[string]any) { b["runtime"] = "evil@sha256:00" }, 400},
		{"unknown partition", func(b map[string]any) { b["partition"] = "tpu" }, 400},
		{"three gpus", func(b map[string]any) { b["resources"] = map[string]any{"gpus": 3} }, 422},
		{"too many cpus", func(b map[string]any) { b["resources"] = map[string]any{"cpus": 27} }, 422},
		{"too long", func(b map[string]any) { b["time_limit_seconds"] = 86401 }, 422},
		{"bigger than node", func(b map[string]any) { b["resources"] = map[string]any{"gpus": 2, "cpus": 40} }, 422},
		{"not a member", func(b map[string]any) { b["team"] = "team-b"; b["workdir"] = "/mist/teams/team-b/bob" }, 403},
	}
	for _, c := range cases {
		b := batch("team-a")
		c.edit(b)
		if r := e.submit("bob", b); r.status != c.status {
			t.Errorf("%s: got %d %s, want %d", c.name, r.status, r.body, c.status)
		}
	}
	var n int
	e.s.Jobs.Pool.QueryRow(e.ctx, "SELECT count(*) FROM jobs").Scan(&n)
	if n != 0 {
		t.Fatalf("%d rejected submissions were stored", n)
	}

	e.id.Unavailable = true
	if r := e.submit("bob", batch("team-a")); r.status != http.StatusServiceUnavailable {
		t.Fatalf("identity outage: %d", r.status)
	}
}

// M1-09: non-team callers get not_found on detail, log and cancel; the
// authorization matrix follows api.md.
func TestAuthorizationMatrix(t *testing.T) {
	e := newEnv(t)
	aliceJob := e.mustSubmit("alice", batch("team-a"))
	bobJob := e.mustSubmit("bob", batch("team-a"))
	path := func(id int64, rest string) string { return fmt.Sprintf("/v1/jobs/%d%s", id, rest) }

	for _, u := range []string{"alice", "bob", "root-admin"} {
		if r := e.do("GET", path(aliceJob.ID, "?script=1"), u, nil); r.status != 200 {
			t.Errorf("%s view: %d", u, r.status)
		}
	}
	for _, rest := range []string{"", "?script=1", "/log"} {
		if r := e.do("GET", path(aliceJob.ID, rest), "carol", nil); r.status != 404 || r.code(t) != CodeNotFound {
			t.Errorf("carol GET %s: %d %s", rest, r.status, r.body)
		}
	}
	if r := e.do("GET", path(99999, ""), "carol", nil); r.status != 404 {
		t.Errorf("missing job: %d", r.status)
	}
	if r := e.do("POST", path(aliceJob.ID, "/cancel"), "carol", nil); r.status != 404 {
		t.Errorf("carol cancel: %d", r.status)
	}

	// can_cancel is computed per caller.
	var seen Job
	e.do("GET", path(aliceJob.ID, ""), "bob", nil).decode(t, &seen)
	if seen.CanCancel {
		t.Error("bob may not cancel alice's job")
	}
	if r := e.do("POST", path(aliceJob.ID, "/cancel"), "bob", nil); r.status != 403 {
		t.Errorf("bob cancel alice: %d", r.status)
	}
	// Alice leads team-a, so she can cancel Bob's job.
	r := e.do("POST", path(bobJob.ID, "/cancel"), "alice", nil)
	var cancelled Job
	r.decode(t, &cancelled)
	if r.status != 200 || cancelled.State != "CANCELLED" || cancelled.CanCancel {
		t.Fatalf("lead cancel: %d %s", r.status, r.body)
	}
	if r := e.do("POST", path(bobJob.ID, "/cancel"), "bob", nil); r.status != 200 {
		t.Errorf("repeat cancel: %d", r.status)
	}

	// Former membership or ownership alone never grants access.
	e.id.RemoveMember("bob", "team-a")
	if r := e.do("GET", path(bobJob.ID, ""), "bob", nil); r.status != 404 {
		t.Errorf("bob after removal: %d", r.status)
	}
	var mine List[Job]
	e.do("GET", "/v1/jobs", "bob", nil).decode(t, &mine)
	if len(mine.Items) != 0 {
		t.Errorf("bob's list after removal: %+v", mine.Items)
	}
	e.id.Disable("alice")
	if r := e.do("GET", path(aliceJob.ID, ""), "alice", nil); r.status != 404 {
		t.Errorf("disabled alice: %d", r.status)
	}

	if r := e.do("GET", "/v1/jobs", "", nil); r.status != 401 || r.code(t) != CodeUnauthenticated {
		t.Errorf("anonymous: %d", r.status)
	}
	if r := e.do("GET", "/v1/jobs?all=1", "carol", nil); r.status != 403 {
		t.Errorf("non-admin all: %d", r.status)
	}
	var all List[Job]
	e.do("GET", "/v1/jobs?all=1", "root-admin", nil).decode(t, &all)
	if len(all.Items) != 2 {
		t.Errorf("admin all: %d jobs", len(all.Items))
	}
}

func TestListPagination(t *testing.T) {
	e := newEnv(t)
	for range 5 {
		e.mustSubmit("alice", batch("team-a"))
	}
	e.mustSubmit("carol", batch("team-b"))
	var ids []int64
	cursor := ""
	for {
		var page List[Job]
		e.do("GET", "/v1/jobs?team=team-a&limit=2"+cursor, "bob", nil).decode(t, &page)
		for _, j := range page.Items {
			ids = append(ids, j.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *page.NextCursor
	}
	if len(ids) != 5 || ids[0] < ids[4] {
		t.Fatalf("pages: %v", ids)
	}
	if r := e.do("GET", "/v1/jobs?team=team-b", "bob", nil); r.status != 403 {
		t.Fatalf("other team list: %d", r.status)
	}
	var running List[Job]
	e.do("GET", "/v1/jobs?state=RUNNING", "alice", nil).decode(t, &running)
	if len(running.Items) != 0 {
		t.Fatal("state filter")
	}
}

// M1-09: the queue shows other teams' jobs without embedded identities.
func TestQueueAnonymised(t *testing.T) {
	e := newEnv(t)
	secret := batch("team-a")
	secret["name"] = "alice-secret-project"
	secret["env"] = map[string]string{"WANDB_API_KEY": "hunter2"}
	a := e.mustSubmit("alice", secret)
	c := e.mustSubmit("carol", batch("team-b"))

	r := e.do("GET", "/v1/queue", "carol", nil)
	for _, leak := range []string{"alice", "team-a", "secret", "hunter2", "/mist/"} {
		if bytes.Contains(r.body, []byte(leak)) {
			t.Errorf("queue leaks %q: %s", leak, r.body)
		}
	}
	var q List[QueueEntry]
	r.decode(t, &q)
	if len(q.Items) != 2 {
		t.Fatalf("queue: %s", r.body)
	}
	for _, it := range q.Items {
		switch it.ID {
		case a.ID:
			if it.User != nil || it.Team != nil || it.Name != nil || it.Resources.GPUs != 1 {
				t.Errorf("other team entry: %+v", it)
			}
		case c.ID:
			if it.User == nil || *it.User != "carol" || it.Workdir != nil {
				t.Errorf("own entry: %+v", it)
			}
		}
	}
	// Bob is in team-a and sees names there.
	e.do("GET", "/v1/queue?partition=gpu", "bob", nil).decode(t, &q)
	for _, it := range q.Items {
		if it.ID == a.ID && (it.User == nil || *it.User != "alice") {
			t.Errorf("team member entry: %+v", it)
		}
	}
}

func TestPartitions(t *testing.T) {
	e := newEnv(t)
	if _, err := e.s.Jobs.Pool.Exec(e.ctx,
		"INSERT INTO limits VALUES ('user', 'carol', 'gpu', '*', 'gpus', 2), ('team', 'team-b', 'gpu', '*', 'gpus', 3)"); err != nil {
		t.Fatal(err)
	}
	r := e.do("GET", "/v1/partitions", "bob", nil)
	if bytes.Contains(r.body, []byte("carol")) || bytes.Contains(r.body, []byte("team-b")) {
		t.Fatalf("other subjects' limits shown: %s", r.body)
	}
	var ps List[Partition]
	r.decode(t, &ps)
	var gpu *Partition
	for i := range ps.Items {
		if ps.Items[i].Name == "gpu" {
			gpu = &ps.Items[i]
		}
	}
	if gpu == nil || !gpu.Default || gpu.Free.GPUs != 2 || gpu.DefaultCPUs != 8 || !gpu.PerAccelerator || len(gpu.Limits) != 3 {
		t.Fatalf("gpu partition: %+v", gpu)
	}
	e.do("GET", "/v1/partitions", "carol", nil).decode(t, &ps)
	for _, p := range ps.Items {
		if p.Name == "gpu" && len(p.Limits) != 5 {
			t.Fatalf("carol's limits: %+v", p.Limits)
		}
	}
}

func TestLogs(t *testing.T) {
	e := newEnv(t)
	j := e.mustSubmit("alice", batch("team-a"))
	var l Log
	e.do("GET", fmt.Sprintf("/v1/jobs/%d/log", j.ID), "bob", nil).decode(t, &l)
	if l.Text != "" || l.State != "PENDING" || l.OutputPath != j.OutputPath {
		t.Fatalf("pending log: %+v", l)
	}

	// Pretend it was admitted and wrote a lot of output.
	if _, err := e.s.Jobs.Pool.Exec(e.ctx, "UPDATE jobs SET node = 'utmist-z1opa08', state = 'RUNNING' WHERE id = $1", j.ID); err != nil {
		t.Fatal(err)
	}
	e.logs.text[j.ID] = strings.Repeat("a", 100<<10) + "the end\n"
	e.do("GET", fmt.Sprintf("/v1/jobs/%d/log", j.ID), "alice", nil).decode(t, &l)
	if len(l.Text) != tailBytes || !l.Truncated || !strings.HasSuffix(l.Text, "the end\n") {
		t.Fatalf("tail: %d bytes, truncated %v", len(l.Text), l.Truncated)
	}

	// Follow over a WebSocket.
	wsURL := "ws" + strings.TrimPrefix(e.srv.URL, "http") + fmt.Sprintf("/v1/jobs/%d/log?follow=1", j.ID)
	cfg, _ := websocket.NewConfig(wsURL, "https://mist.example")
	cfg.Header.Set("X-Test-User", "alice")
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		var frame []byte
		if err := websocket.Message.Receive(conn, &frame); err != nil {
			break
		}
		got = append(got, frame...)
	}
	conn.Close()
	if string(got) != e.logs.text[j.ID] {
		t.Fatalf("followed %d bytes", len(got))
	}

	cfg, _ = websocket.NewConfig(wsURL, "https://evil.example")
	cfg.Header.Set("X-Test-User", "alice")
	if _, err := websocket.DialConfig(cfg); err == nil {
		t.Fatal("disallowed origin accepted")
	}
}

// An open stream ends when the caller loses access.
func TestFollowRevoked(t *testing.T) {
	e := newEnv(t)
	j := e.mustSubmit("alice", batch("team-a"))
	e.s.Jobs.Pool.Exec(e.ctx, "UPDATE jobs SET node = 'utmist-z1opa08', state = 'RUNNING' WHERE id = $1", j.ID)
	e.s.Logs = blockingLogs{}
	wsURL := "ws" + strings.TrimPrefix(e.srv.URL, "http") + fmt.Sprintf("/v1/jobs/%d/log?follow=1", j.ID)
	cfg, _ := websocket.NewConfig(wsURL, "https://mist.example")
	cfg.Header.Set("X-Test-User", "bob")
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	e.id.RemoveMember("bob", "team-a")
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame []byte
	if err := websocket.Message.Receive(conn, &frame); err == nil || strings.Contains(err.Error(), "timeout") {
		t.Fatalf("stream still open after revocation: %v", err)
	}
}

type blockingLogs struct{}

func (blockingLogs) Logs(ctx context.Context, _ int64, _ bool) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() { <-ctx.Done(); pw.Close() }()
	return pr, nil
}

// The production build has no development authenticator and rejects its
// header outright, even alongside valid credentials.
func TestProductionRejectsDevHeader(t *testing.T) {
	e := newEnv(t)
	r := e.do("GET", "/v1/jobs", "alice", nil, DevUserHeader, "root-admin")
	if r.status != 401 || !bytes.Contains(r.body, []byte("development")) {
		t.Fatalf("dev header: %d %s", r.status, r.body)
	}
	r = e.do("GET", "/v1/jobs", "", nil, DevUserHeader, "root-admin")
	if r.status != 401 {
		t.Fatalf("dev header alone: %d", r.status)
	}
}

func TestCleanupEndpoint(t *testing.T) {
	e := newEnv(t)
	receipt := k3s.CleanupReceipt{ExecutionID: "3f1c2a9e-8b7d-4c6e-9a5b-1d2e3f4a5b6c", PodUID: "u", Node: "utmist-tt", Evidence: "gone"}
	url := "/internal/v1/executions/" + receipt.ExecutionID + "/cleanup"
	post := func(token string, rc k3s.CleanupReceipt, path string) reply {
		hdr := []string{}
		if token != "" {
			hdr = []string{"Authorization", "Bearer " + token}
		}
		return e.do("POST", path, "", rc, hdr...)
	}
	if r := post("", receipt, url); r.status != 401 {
		t.Errorf("no token: %d", r.status)
	}
	if r := post("tt-provision", receipt, url); r.status != 401 {
		t.Errorf("token for another scope: %d", r.status)
	}
	// Account credentials do not reach /internal.
	if r := e.do("POST", url, "root-admin", receipt); r.status != 401 {
		t.Errorf("account on internal: %d", r.status)
	}
	other := receipt
	other.Node = "utmist-z1opa08"
	if r := post("tt-secret", other, url); r.status != 403 {
		t.Errorf("report for another node: %d", r.status)
	}
	if r := post("tt-secret", receipt, "/internal/v1/executions/other/cleanup"); r.status != 400 {
		t.Errorf("path mismatch: %d", r.status)
	}
	if r := post("tt-secret", receipt, url); r.status != 200 || len(e.cleanup.got) != 1 {
		t.Errorf("valid receipt: %d %s", r.status, r.body)
	}
	e.cleanup.fail = fmt.Errorf("%w: Pod job-1 still exists", k3s.ErrCleanupRejected)
	if r := post("tt-secret", receipt, url); r.status != 409 {
		t.Errorf("rejected receipt: %d", r.status)
	}
	// Host tokens do not reach /v1.
	if r := e.do("GET", "/v1/jobs", "", nil, "Authorization", "Bearer tt-secret"); r.status != 401 {
		t.Errorf("host on v1: %d", r.status)
	}
}
