package main

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Physical per-team dataset capacity is the upload limit; no small fixed ceiling.
const maxDatasetBytes int64 = 1 << 50

var datasetIDPattern = regexp.MustCompile(`^dataset-[a-f0-9]{24}$`)

type SharedStorage struct {
	root        string
	modelRoot   string
	mu          sync.Mutex
	uploadMu    sync.Mutex
	uploadLimit int64
}
type Dataset struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Filename    string    `json:"filename"`
	Owner       string    `json:"owner"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	Created     time.Time `json:"created"`
	State       string    `json:"state"`
	Files       int       `json:"files"`
	TeamID      string    `json:"team_id,omitempty"`
	TeamName    string    `json:"team_name,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	MemberName  string    `json:"member_name,omitempty"`
	ContentPath string    `json:"content_path,omitempty"`
	Shared      bool      `json:"shared,omitempty"`
}

func NewSharedStorage(root string) (*SharedStorage, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("storage root must be absolute")
	}
	for _, dir := range []string{"metadata/datasets", ".uploads", "datasets", "jobs", "legacy"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return nil, err
		}
	}
	limit := maxDatasetBytes
	if configured := os.Getenv("MIST_MAX_DATASET_GIB"); configured != "" {
		gib, err := strconv.ParseInt(configured, 10, 64)
		if err != nil || gib < 1 || gib > maxDatasetBytes/(1024*1024*1024) {
			return nil, errors.New("MIST_MAX_DATASET_GIB is outside the supported range")
		}
		limit = gib * 1024 * 1024 * 1024
	}
	return &SharedStorage{root: root, uploadLimit: limit}, nil
}

func (s *SharedStorage) availableUploadLimit(team bool) int64 {
	var stats syscall.Statfs_t
	if syscall.Statfs(s.root, &stats) != nil {
		return 0
	}
	reserve := int64(1024 * 1024 * 1024)
	if team {
		reserve = 64 * 1024 * 1024
	}
	return max(0, min(s.uploadLimit, int64(stats.Bavail)*stats.Bsize-reserve))
}
func (s *SharedStorage) dataset(id, owner string) (*Dataset, error) {
	if !datasetIDPattern.MatchString(id) {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(s.root, "metadata/datasets", id+".json"))
	if err != nil {
		return nil, err
	}
	var ds Dataset
	if err = json.Unmarshal(data, &ds); err != nil {
		return nil, err
	}
	if ds.ID != id || ds.Owner != owner || ds.State != "Ready" {
		return nil, os.ErrNotExist
	}
	return &ds, nil
}
func (s *SharedStorage) resultsRoot() string {
	if s.modelRoot != "" {
		return s.modelRoot
	}
	return s.root
}
func (s *SharedStorage) prepareJob(id string) error {
	dir := filepath.Join(s.resultsRoot(), "jobs", id, "outputs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.Chmod(dir, 0777)
}
func safeRelative(path string) bool {
	return path != "" && path != "." && filepath.IsLocal(path) && !strings.ContainsAny(path, "\\\x00") && filepath.Clean(path) == path
}
func (s *SharedStorage) upload(r *http.Request, owner string) (*Dataset, error) {
	filename := r.URL.Query().Get("filename")
	if !safeRelative(filename) || filepath.Base(filename) != filename || len(filename) > 200 {
		return nil, &submissionError{errors.New("invalid upload filename")}
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = filename
	}
	if len(name) > 120 {
		return nil, &submissionError{errors.New("dataset name exceeds 120 characters")}
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "zip" {
		return nil, &submissionError{errors.New("format must be zip or omitted")}
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	var stats syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stats); err != nil {
		return nil, err
	}
	reserve := int64(1024 * 1024 * 1024)
	if r.Context().Value(teamContextKey{}) != nil {
		reserve = 64 * 1024 * 1024
	}
	remaining := int64(stats.Bavail)*stats.Bsize - reserve
	if remaining <= 0 {
		return nil, fmt.Errorf("dataset storage has insufficient free space: %w", syscall.ENOSPC)
	}
	limit := min(s.uploadLimit, remaining)
	if r.ContentLength > limit {
		return nil, &submissionError{errors.New("dataset exceeds upload limit or available storage")}
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	id := "dataset-" + hex.EncodeToString(random)
	stage := filepath.Join(s.root, ".uploads", id)
	if err := os.Mkdir(stage, 0700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	upload := filepath.Join(stage, "upload")
	file, err := os.OpenFile(upload, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	checksum := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, checksum), io.LimitReader(r.Body, limit+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = r.Context().Err(); err != nil {
		return nil, err
	}
	if n > limit {
		return nil, &submissionError{errors.New("dataset exceeds upload limit or available storage")}
	}
	if n == 0 {
		return nil, &submissionError{errors.New("dataset is empty")}
	}
	content := filepath.Join(stage, "content")
	if err = os.Mkdir(content, 0755); err != nil {
		return nil, err
	}
	count, size := 1, n
	if format == "zip" {
		count, size, err = extractDatasetZip(upload, content, min(s.uploadLimit, remaining-n))
		if err != nil {
			return nil, &submissionError{err}
		}
	} else {
		if err = os.Rename(upload, filepath.Join(content, filename)); err != nil {
			return nil, err
		}
		if err = os.Chmod(filepath.Join(content, filename), 0644); err != nil {
			return nil, err
		}
	}
	ds := &Dataset{ID: id, Name: name, Filename: filename, Owner: owner, Size: size, SHA256: hex.EncodeToString(checksum.Sum(nil)), Created: time.Now().UTC(), State: "Ready", Files: count}
	relative := "datasets/" + id
	access, _ := r.Context().Value(teamContextKey{}).(*TeamAccess)
	if access != nil {
		access.Service.mu.Lock()
		defer access.Service.mu.Unlock()
		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = access.Member.ID
		}
		if err = access.Service.canWrite(r.Context(), access, scope); err != nil {
			return nil, err
		}
		ds.TeamID, ds.Scope, ds.MemberName = access.Team.ID, scope, access.Member.Name
		relative = scopePath(scope) + "/datasets/" + id
		ds.ContentPath = relative + "/content"
	}
	target := filepath.Join(s.root, relative)
	if err = os.MkdirAll(target, 0755); err != nil {
		return nil, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(target)
		}
	}()
	if err = os.Rename(content, filepath.Join(target, "content")); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(ds)
	temp := filepath.Join(stage, "metadata.json")
	if err = os.WriteFile(temp, data, 0600); err != nil {
		return nil, err
	}
	if err = os.Rename(temp, filepath.Join(s.root, "metadata/datasets", id+".json")); err != nil {
		return nil, err
	}
	published = true
	return ds, nil
}
func extractDatasetZip(path, destination string, limit int64) (int, int64, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return 0, 0, errors.New("invalid ZIP archive")
	}
	defer archive.Close()
	if len(archive.File) > 10000 {
		return 0, 0, errors.New("ZIP contains too many entries")
	}
	var total int64
	files := 0
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !safeRelative(name) || entry.Mode()&os.ModeSymlink != 0 || (!entry.FileInfo().IsDir() && !entry.Mode().IsRegular()) {
			return 0, 0, errors.New("ZIP contains an unsafe path or special file")
		}
		target := filepath.Join(destination, name)
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(target, 0755); err != nil {
				return 0, 0, err
			}
			continue
		}
		if entry.UncompressedSize64 > uint64(max(int64(0), limit-total)) {
			return 0, 0, errors.New("expanded ZIP exceeds dataset limit")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return 0, 0, err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return 0, 0, err
		}
		reader, err := entry.Open()
		if err != nil {
			output.Close()
			return 0, 0, err
		}
		n, copyErr := io.Copy(output, io.LimitReader(reader, limit-total+1))
		reader.Close()
		closeErr := output.Close()
		total += n
		if copyErr != nil {
			return 0, 0, copyErr
		}
		if closeErr != nil {
			return 0, 0, closeErr
		}
		if total > limit {
			return 0, 0, errors.New("expanded ZIP exceeds dataset limit")
		}
		files++
	}
	if files == 0 {
		return 0, 0, errors.New("ZIP contains no files")
	}
	return files, total, nil
}
func (a *App) storageInfo(w http.ResponseWriter, r *http.Request) {
	e := a.requestExecutor(r)
	s := e.storage
	if s == nil {
		writeJSON(w, 200, map[string]interface{}{"enabled": false})
		return
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.root, &stat); err != nil {
		executorError(w, err)
		return
	}
	info := map[string]interface{}{"enabled": true, "capacity_bytes": stat.Blocks * uint64(stat.Bsize), "available_bytes": stat.Bavail * uint64(stat.Bsize), "upload_limit_bytes": s.availableUploadLimit(e.team != nil), "max_dataset_bytes": s.uploadLimit}
	if e.team != nil {
		var models syscall.Statfs_t
		if err := syscall.Statfs(s.resultsRoot(), &models); err != nil {
			executorError(w, err)
			return
		}
		info["model_capacity_bytes"], info["model_available_bytes"] = models.Blocks*uint64(models.Bsize), models.Bavail*uint64(models.Bsize)
		info["dataset_allocation_gib"], info["model_allocation_gib"] = e.team.Team.Policy.StorageGiB, e.team.Team.Policy.ModelStorageGiB
	}
	writeJSON(w, 200, info)
}
func (a *App) datasets(w http.ResponseWriter, r *http.Request) {
	e := a.requestExecutor(r)
	s := e.storage
	if s == nil {
		writeJSON(w, 503, map[string]string{"error": "Shared storage is not configured"})
		return
	}
	if e.team != nil {
		a.teamDatasets(w, r, e)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		entries, err := os.ReadDir(filepath.Join(s.root, "metadata/datasets"))
		if err != nil {
			executorError(w, err)
			return
		}
		list := []Dataset{}
		for _, entry := range entries {
			if ds, err := s.dataset(strings.TrimSuffix(entry.Name(), ".json"), e.owner); err == nil {
				list = append(list, *ds)
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Created.After(list[j].Created) })
		writeJSON(w, 200, map[string]interface{}{"datasets": list, "upload_limit_bytes": s.availableUploadLimit(false)})
	case http.MethodPost:
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(24 * time.Hour))
		_ = controller.SetWriteDeadline(time.Now().Add(24 * time.Hour))
		r.Body = http.MaxBytesReader(w, r.Body, s.uploadLimit+1)
		ds, err := s.upload(r, e.owner)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 201, ds)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}
func (a *App) dataset(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/files") {
		a.datasetFiles(w, r)
		return
	}
	e := a.requestExecutor(r)
	s := e.storage
	if s == nil {
		writeJSON(w, 503, map[string]string{"error": "Shared storage is not configured"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/datasets/")
	if e.team != nil {
		a.teamDataset(w, r, e, id)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, err := s.dataset(id, e.owner)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "dataset not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, ds)
	case http.MethodDelete:
		jobs, err := e.client.BatchV1().Jobs(e.namespace).List(r.Context(), metav1.ListOptions{LabelSelector: managedLabel + "=mist,mist.io/owner=" + e.owner})
		if err != nil {
			executorError(w, err)
			return
		}
		for _, job := range jobs.Items {
			var req CreateJobRequest
			_ = json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req)
			if req.DatasetID == id && !terminal(&job) {
				if job.Annotations[cancelAnnotation] != "" {
					pods, err := e.client.CoreV1().Pods(e.namespace).List(r.Context(), metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + job.Name})
					if err != nil {
						executorError(w, err)
						return
					}
					if len(pods.Items) == 0 {
						continue
					}
				}
				writeJSON(w, 409, map[string]string{"error": "Dataset is still referenced by an active or terminating job"})
				return
			}
		}
		if err = os.Remove(filepath.Join(s.root, "metadata/datasets", id+".json")); err != nil {
			executorError(w, err)
			return
		}
		if err = os.RemoveAll(filepath.Join(s.root, "datasets", id)); err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"id": id, "state": "Deleted"})
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}
func (s *SharedStorage) outputPath(job *batchv1.Job) (string, error) {
	if path := job.Annotations["mist.io/output-path"]; path != "" {
		if !safeRelative(path) {
			return "", errors.New("invalid job storage path")
		}
		return filepath.Join(s.resultsRoot(), path), nil
	}
	if job.Annotations["mist.io/storage"] == "shared-v1" {
		return filepath.Join(s.resultsRoot(), "jobs", job.Name, "outputs"), nil
	}
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.Name == "checkpoints" && vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == "mist-shared-jobs" {
			return filepath.Join(s.resultsRoot(), "jobs", job.Name, "outputs"), nil
		}
	}
	var req CreateJobRequest
	if err := json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req); err != nil {
		return "", err
	}
	if req.Accelerator != "cpu" && req.Accelerator != "nvidia" && req.Accelerator != "tenstorrent" {
		return "", errors.New("unknown legacy output storage")
	}
	return filepath.Join(s.resultsRoot(), "legacy", req.Accelerator, job.Name), nil
}

type ResultFile struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func listResultFiles(root *os.Root) ([]ResultFile, error) {
	files := []ResultFile{}
	visited := 0
	var visit func(string, int) error
	visit = func(path string, depth int) error {
		if depth > 20 {
			return errors.New("output directory exceeds 20 levels")
		}
		dir, err := root.Open(path)
		if err != nil {
			return err
		}
		defer dir.Close()
		for {
			entries, readErr := dir.ReadDir(128)
			for _, entry := range entries {
				visited++
				if visited > 10000 {
					return errors.New("output directory exceeds 10000 entries")
				}
				name := filepath.Join(path, entry.Name())
				info, err := root.Lstat(name)
				if err != nil {
					return err
				}
				if info.IsDir() {
					if err = visit(name, depth+1); err != nil {
						return err
					}
				} else if info.Mode().IsRegular() {
					files = append(files, ResultFile{name, info.Size(), info.ModTime()})
				}
				if len(files) > 5000 {
					return errors.New("output directory exceeds 5000 files")
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		return nil
	}
	err := visit(".", 0)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}
func (a *App) jobFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[1] != "files" || (len(parts) == 3 && parts[2] != "download") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	e := a.requestExecutor(r)
	job, err := e.managedJob(r.Context(), parts[0])
	if err != nil {
		executorError(w, err)
		return
	}
	if e.storage == nil {
		writeJSON(w, 503, map[string]string{"error": "Shared storage is not configured"})
		return
	}
	path, err := e.storage.outputPath(job)
	if err != nil {
		executorError(w, err)
		return
	}
	serveStoredFiles(w, r, path, len(parts) == 3)
}
func serveStoredFiles(w http.ResponseWriter, r *http.Request, path string, download bool) {
	root, err := os.OpenRoot(path)
	if os.IsNotExist(err) {
		if !download {
			writeJSON(w, 200, map[string]interface{}{"files": []ResultFile{}})
		} else {
			writeJSON(w, 404, map[string]string{"error": "file not found"})
		}
		return
	}
	if err != nil {
		executorError(w, err)
		return
	}
	defer root.Close()
	if !download {
		files, err := listResultFiles(root)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"files": files})
		return
	}
	filename := r.URL.Query().Get("path")
	if !safeRelative(filename) {
		writeJSON(w, 400, map[string]string{"error": "Invalid file path"})
		return
	}
	file, err := root.Open(filename)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "file not found"})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeJSON(w, 404, map[string]string{"error": "file not found"})
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(24 * time.Hour))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(filename)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, filename, info.ModTime(), file)
}
