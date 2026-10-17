package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func uploadTest(t *testing.T, s *SharedStorage, path string, data []byte) (*Dataset, error) {
	t.Helper()
	return s.upload(httptest.NewRequest("POST", path, bytes.NewReader(data)), "test")
}
func TestSharedDatasetAtomicLimitsAndOwnership(t *testing.T) {
	s, err := NewSharedStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.uploadLimit = 16
	ds, err := uploadTest(t, s, "/datasets?filename=data.csv", []byte("1,2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ds.State != "Ready" || ds.Files != 1 || ds.Size != 4 || len(ds.SHA256) != 64 {
		t.Fatalf("bad dataset %+v", ds)
	}
	if _, err = s.dataset(ds.ID, "other"); err == nil {
		t.Fatal("another member can access dataset")
	}
	if _, err = uploadTest(t, s, "/datasets?filename=data.csv", bytes.Repeat([]byte("a"), 17)); err == nil {
		t.Fatal("oversized upload accepted")
	}
	if _, err = uploadTest(t, s, "/datasets?filename=../escape", []byte("a")); err == nil {
		t.Fatal("unsafe filename accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(s.root, ".uploads"))
	if len(entries) != 0 {
		t.Fatal("failed upload retained staging files")
	}
	entries, _ = os.ReadDir(filepath.Join(s.root, "metadata/datasets"))
	if len(entries) != 1 {
		t.Fatal("failed upload published metadata")
	}
}

func TestSharedDatasetConfiguredCeilingAndMissingStorage(t *testing.T) {
	t.Setenv("MIST_MAX_DATASET_GIB", "4")
	root := t.TempDir()
	s, err := NewSharedStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	// Reject an oversized declared body without reading or staging gigabytes.
	request := httptest.NewRequest("POST", "/datasets?filename=data.bin", strings.NewReader("small"))
	request.ContentLength = 5 * 1024 * 1024 * 1024
	if _, err = s.upload(request, "test"); err == nil {
		t.Fatal("configured dataset ceiling was bypassed")
	}
	if limit := s.availableUploadLimit(true); limit < 0 || limit > 4*1024*1024*1024 {
		t.Fatalf("unsafe advertised upload limit: %d", limit)
	}
	if err = os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if s.availableUploadLimit(true) != 0 {
		t.Fatal("missing storage advertised upload capacity")
	}
	for _, value := range []string{"0", "1048577", "invalid"} {
		t.Setenv("MIST_MAX_DATASET_GIB", value)
		if _, err = NewSharedStorage(t.TempDir()); err == nil {
			t.Fatalf("accepted invalid upload ceiling %q", value)
		}
	}
}
func TestSharedZipRejectsTraversalSymlinksAndExpansion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  os.FileMode
		data  string
		limit int64
	}{{"../escape", 0644, "x", 100}, {"symlink", os.ModeSymlink | 0777, "/etc/passwd", 100}, {"data", 0644, strings.Repeat("x", 1000), 10}} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		header := &zip.FileHeader{Name: tc.name, Method: zip.Deflate}
		header.SetMode(tc.mode)
		file, _ := writer.CreateHeader(header)
		_, _ = io.WriteString(file, tc.data)
		writer.Close()
		dir := t.TempDir()
		path := filepath.Join(dir, "test.zip")
		os.WriteFile(path, buffer.Bytes(), 0600)
		if _, _, err := extractDatasetZip(path, filepath.Join(dir, "out"), tc.limit); err == nil {
			t.Fatalf("accepted unsafe archive %s", tc.name)
		}
	}
}
func TestSharedJobMountAndEscapingOutput(t *testing.T) {
	e := testExecutor()
	s, _ := NewSharedStorage(t.TempDir())
	e.storage = s
	e.sharedPVC = "mist-shared-jobs"
	ds, err := uploadTest(t, s, "/datasets?filename=data.csv", []byte("1,2\n"))
	if err != nil {
		t.Fatal(err)
	}
	status, err := e.submit(context.Background(), CreateJobRequest{Command: []string{"true"}, DatasetID: ds.ID})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := e.managedJob(context.Background(), status.ID)
	spec := job.Spec.Template.Spec
	if len(spec.InitContainers) != 0 {
		t.Fatal("shared job exposes full storage in init")
	}
	input := false
	for _, mount := range spec.Containers[0].VolumeMounts {
		if mount.MountPath == "/inputs" {
			input = mount.ReadOnly && mount.SubPath == "datasets/"+ds.ID+"/content"
		}
	}
	if !input {
		t.Fatal("dataset not mounted read-only")
	}
	output, _ := s.outputPath(job)
	os.WriteFile(filepath.Join(output, "model.json"), []byte("{}"), 0644)
	os.Symlink("/etc/passwd", filepath.Join(output, "escape"))
	app := NewKubernetesApp(e, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		path   string
		status int
	}{{"model.json", 200}, {"../metadata", 400}, {"escape", 404}} {
		req := httptest.NewRequest("GET", "/jobs/"+status.ID+"/files/download?path="+tc.path, nil)
		response := httptest.NewRecorder()
		app.httpServer.Handler.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("%s: got %d: %s", tc.path, response.Code, response.Body.String())
		}
	}
	e.owner = "other"
	response := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(response, httptest.NewRequest("GET", "/jobs/"+status.ID+"/files", nil))
	if response.Code != 404 {
		t.Fatal("foreign job files accessible")
	}
}
func TestMemberAuthIsolationAndFailureClosed(t *testing.T) {
	t.Setenv("MIST_AUTH_DATABASE", filepath.Join(t.TempDir(), "auth.sqlite"))
	t.Setenv("MIST_ALLOWED_ORIGINS", "http://mist.test")
	app := NewKubernetesApp(testExecutor(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, m := range []Member{
		{ID: "alice", Name: "Alice", Email: "alice@example.org", Role: "user"},
		{ID: "bob", Name: "Bob", Email: "bob@example.org", Role: "user"},
	} {
		if _, err := app.auth.db.Exec(`
			INSERT INTO user (id, name, email, role, createdAt, updatedAt)
			VALUES (?, ?, ?, ?, ?, ?)
		`, m.ID, m.Name, m.Email, m.Role, now, now); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, cookie, origin, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Mist-Owner", "test")
		response := httptest.NewRecorder()
		app.httpServer.Handler.ServeHTTP(response, req)
		return response
	}
	if response := request("GET", "/jobs", "", "", ""); response.Code != 401 {
		t.Fatal("anonymous request accepted")
	}
	if response := request("POST", "/jobs", "member=alice", "http://evil.test", `{"command":["true"]}`); response.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	if response := request("POST", "/jobs", "member=alice", "http://mist.test", `{"command":["true"]}`); response.Code != 201 {
		t.Fatalf("submit failed %d %s", response.Code, response.Body.String())
	}
	if response := request("GET", "/jobs", "member=bob", "", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"count":0`) {
		t.Fatalf("another member sees jobs: %s", response.Body.String())
	}
	app.auth.Close()
	if response := request("GET", "/jobs", "member=alice", "", ""); response.Code != 503 {
		t.Fatal("auth failure did not close access")
	}
}
