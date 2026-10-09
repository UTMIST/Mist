package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scopePath(scope string) string {
	if scope == "common" {
		return "common"
	}
	return "members/" + hashID(scope)
}

type StorageFolder struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
	Scope    string `json:"scope"`
	Name     string `json:"name"`
	Shared   bool   `json:"shared"`
	Writable bool   `json:"writable"`
}

func (a *App) storageFolders(w http.ResponseWriter, r *http.Request) {
	e := a.requestExecutor(r)
	if e.team == nil {
		writeJSON(w, 200, map[string]any{"folders": []StorageFolder{}})
		return
	}
	_, teams, err := a.teams.registry(r.Context())
	if err != nil {
		executorError(w, err)
		return
	}
	folders := []StorageFolder{}
	for _, source := range teams {
		if source.Disabled {
			continue
		}
		scopes := map[string]string{}
		if source.ID == e.team.Team.ID {
			scopes["common"] = "Common"
			for _, m := range source.Members {
				scopes[m.ID] = m.Name
			}
			jobs, err := e.client.BatchV1().Jobs(e.namespace).List(r.Context(), metav1.ListOptions{LabelSelector: managedLabel + "=mist"})
			if err != nil {
				executorError(w, err)
				return
			}
			for _, job := range jobs.Items {
				scope := job.Annotations["mist.io/storage-scope"]
				if scope != "" && scope != "common" {
					if _, exists := scopes[scope]; !exists {
						scopes[scope] = job.Annotations["mist.io/creator-name"] + " (retained files)"
					}
				}
			}
			list, err := a.teams.accessibleDatasets(r.Context(), e.team.Team)
			if err != nil {
				executorError(w, err)
				return
			}
			for _, ds := range list {
				if ds.TeamID == source.ID && ds.Scope != "" {
					if _, ok := scopes[ds.Scope]; !ok {
						scopes[ds.Scope] = ds.MemberName + " (retained files)"
					}
				}
			}
		}
		for _, g := range source.Grants {
			if g.TargetTeam == e.team.Team.ID {
				if g.Scope == "common" {
					scopes[g.Scope] = "Common"
				} else if m := source.member(g.Scope); m != nil {
					scopes[g.Scope] = m.Name
				} else {
					scopes[g.Scope] = "Former member"
				}
			}
		}
		for scope, name := range scopes {
			folders = append(folders, StorageFolder{TeamID: source.ID, TeamName: source.Name, Scope: scope, Name: name, Shared: source.ID != e.team.Team.ID, Writable: source.ID == e.team.Team.ID && a.teams.canWrite(r.Context(), e.team, scope) == nil})
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		if folders[i].TeamName == folders[j].TeamName {
			return folders[i].Name < folders[j].Name
		}
		return folders[i].TeamName < folders[j].TeamName
	})
	writeJSON(w, 200, map[string]any{"folders": folders})
}
func (a *App) storageFolderFiles(w http.ResponseWriter, r *http.Request) {
	e := a.requestExecutor(r)
	if e.team == nil {
		writeJSON(w, 400, map[string]string{"error": "Select a team workspace"})
		return
	}
	sourceID, scope := r.URL.Query().Get("source_team"), r.URL.Query().Get("scope")
	if sourceID == "" {
		sourceID = e.team.Team.ID
	}
	source, err := a.teams.team(r.Context(), sourceID)
	if err != nil || source.Disabled {
		writeJSON(w, 404, map[string]string{"error": "folder not found"})
		return
	}
	authorized := sourceID == e.team.Team.ID
	if !authorized {
		for _, grant := range source.Grants {
			if grant.TargetTeam == e.team.Team.ID && grant.Scope == scope {
				authorized = true
			}
		}
	}
	if !authorized || (scope != "common" && (scope == "" || len(scope) > 128 || strings.ContainsAny(scope, "/\\\x00"))) {
		writeJSON(w, 404, map[string]string{"error": "folder not found"})
		return
	}
	store, err := a.teams.store(source)
	if err != nil {
		executorError(w, err)
		return
	}
	serveTeamFolder(w, r, store, scope)
}
func (s *TeamService) resolveDataset(ctx context.Context, target *Team, id string) (*SharedStorage, *Team, *Dataset, error) {
	if !datasetIDPattern.MatchString(id) {
		return nil, nil, nil, os.ErrNotExist
	}
	if store, err := s.store(target); err == nil {
		if ds, err := store.dataset(id, target.ID); err == nil {
			return store, target, ds, nil
		}
	}
	_, teams, err := s.registry(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	for i := range teams {
		source := &teams[i]
		if source.Disabled || source.ID == target.ID {
			continue
		}
		scopes := map[string]bool{}
		for _, g := range source.Grants {
			if g.TargetTeam == target.ID {
				scopes[g.Scope] = true
			}
		}
		if len(scopes) == 0 {
			continue
		}
		store, err := s.store(source)
		if err != nil {
			continue
		}
		ds, err := store.dataset(id, source.ID)
		if err == nil && scopes[ds.Scope] {
			ds.Shared = true
			return store, source, ds, nil
		}
	}
	return nil, nil, nil, os.ErrNotExist
}
func (s *TeamService) accessibleDatasets(ctx context.Context, target *Team) ([]Dataset, error) {
	_, teams, err := s.registry(ctx)
	if err != nil {
		return nil, err
	}
	result := []Dataset{}
	for i := range teams {
		source := &teams[i]
		if source.Disabled {
			continue
		}
		scopes := map[string]bool{}
		for _, g := range source.Grants {
			if g.TargetTeam == target.ID {
				scopes[g.Scope] = true
			}
		}
		if source.ID != target.ID && len(scopes) == 0 {
			continue
		}
		store, err := s.store(source)
		if err != nil {
			if source.ID == target.ID {
				return nil, err
			}
			continue
		}
		entries, err := os.ReadDir(filepath.Join(store.root, "metadata/datasets"))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			ds, err := store.dataset(strings.TrimSuffix(entry.Name(), ".json"), source.ID)
			if err != nil {
				continue
			}
			if source.ID == target.ID || scopes[ds.Scope] {
				ds.TeamName = source.Name
				ds.Shared = source.ID != target.ID
				result = append(result, *ds)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}
func (s *TeamService) canWrite(ctx context.Context, access *TeamAccess, scope string) error {
	t, err := s.team(ctx, access.Team.ID)
	if err != nil {
		return err
	}
	m := t.member(access.Member.ID)
	if t.Disabled || m == nil {
		return &permissionError{errors.New("Team membership was revoked")}
	}
	if scope == access.Member.ID {
		return nil
	}
	if scope == "common" && (m.CommonWriter || isAdmin(access.Member)) {
		return nil
	}
	return &permissionError{errors.New("You can manage your own folder; common-folder writes require permission")}
}
func (a *App) teamDatasets(w http.ResponseWriter, r *http.Request, e *KubernetesExecutor) {
	access := e.team
	switch r.Method {
	case http.MethodGet:
		list, err := a.teams.accessibleDatasets(r.Context(), access.Team)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"datasets": list, "upload_limit_bytes": e.storage.availableUploadLimit(true)})
	case http.MethodPost:
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(24 * time.Hour))
		_ = controller.SetWriteDeadline(time.Now().Add(24 * time.Hour))
		scope := r.URL.Query().Get("scope")
		if scope == "" {
			scope = access.Member.ID
		}
		if err := a.teams.canWrite(r.Context(), access, scope); err != nil {
			executorError(w, err)
			return
		}
		// upload() rechecks membership/permissions before atomically publishing.
		r.Body = http.MaxBytesReader(w, r.Body, e.storage.uploadLimit+1)
		ds, err := e.storage.upload(r, access.Team.ID)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 201, ds)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}
func (a *App) teamDataset(w http.ResponseWriter, r *http.Request, e *KubernetesExecutor, id string) {
	store, source, ds, err := a.teams.resolveDataset(r.Context(), e.team.Team, id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "dataset not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, ds)
	case http.MethodDelete:
		if source.ID != e.team.Team.ID {
			writeJSON(w, 403, map[string]string{"error": "Shared datasets are read-only"})
			return
		}
		a.teams.mu.Lock()
		defer a.teams.mu.Unlock()
		if err = a.teams.canWrite(r.Context(), e.team, ds.Scope); err != nil {
			executorError(w, err)
			return
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		jobs, err := e.client.BatchV1().Jobs("").List(r.Context(), metav1.ListOptions{LabelSelector: managedLabel + "=mist"})
		if err != nil {
			executorError(w, err)
			return
		}
		for _, job := range jobs.Items {
			var req CreateJobRequest
			_ = json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req)
			if req.DatasetID == id && !terminal(&job) {
				pods, err := e.client.CoreV1().Pods(job.Namespace).List(r.Context(), metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + job.Name})
				if err != nil {
					executorError(w, err)
					return
				}
				if job.Annotations[cancelAnnotation] == "" || len(pods.Items) > 0 {
					writeJSON(w, 409, map[string]string{"error": "Dataset is referenced by an active, queued or terminating job"})
					return
				}
			}
		}
		if err = os.Remove(filepath.Join(store.root, "metadata/datasets", id+".json")); err != nil {
			executorError(w, err)
			return
		}
		path, err := store.contentPath(ds)
		if err != nil {
			executorError(w, err)
			return
		}
		if err = os.RemoveAll(filepath.Dir(path)); err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"id": id, "state": "Deleted"})
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}
func (s *SharedStorage) contentPath(ds *Dataset) (string, error) {
	path := ds.ContentPath
	if path == "" {
		path = "datasets/" + ds.ID + "/content"
	}
	if !safeRelative(path) {
		return "", errors.New("invalid dataset storage path")
	}
	return filepath.Join(s.root, path), nil
}
func (a *App) datasetFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/datasets/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[1] != "files" || (len(parts) == 3 && parts[2] != "download") {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	e := a.requestExecutor(r)
	store := e.storage
	if store == nil {
		writeJSON(w, 503, map[string]string{"error": "Storage is unavailable"})
		return
	}
	var ds *Dataset
	var err error
	if e.team != nil {
		store, _, ds, err = a.teams.resolveDataset(r.Context(), e.team.Team, parts[0])
	} else {
		ds, err = store.dataset(parts[0], e.owner)
	}
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "dataset not found"})
		return
	}
	path, err := store.contentPath(ds)
	if err != nil {
		executorError(w, err)
		return
	}
	serveStoredFiles(w, r, path, len(parts) == 3)
}

// Keep the public common/member tree stable while files live in two quota pools.
func serveTeamFolder(w http.ResponseWriter, r *http.Request, store *SharedStorage, scope string) {
	data := filepath.Join(store.root, scopePath(scope))
	models := filepath.Join(store.resultsRoot(), scopePath(scope))
	if r.URL.Query().Get("download") == "true" {
		path := r.URL.Query().Get("path")
		if strings.HasPrefix(path, "jobs/") {
			serveStoredFiles(w, r, models, true)
		} else if strings.HasPrefix(path, "datasets/") {
			serveStoredFiles(w, r, data, true)
		} else {
			writeJSON(w, 400, map[string]string{"error": "Invalid folder file path"})
		}
		return
	}
	files := []ResultFile{}
	for _, path := range []string{filepath.Join(data, "datasets"), filepath.Join(models, "jobs")} {
		root, err := os.OpenRoot(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			executorError(w, err)
			return
		}
		listed, err := listResultFiles(root)
		root.Close()
		if err != nil {
			executorError(w, err)
			return
		}
		prefix := filepath.Base(path) + "/"
		for _, f := range listed {
			f.Path = prefix + f.Path
			files = append(files, f)
		}
		if len(files) > 10000 {
			writeJSON(w, 400, map[string]string{"error": "folder has too many files"})
			return
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	writeJSON(w, 200, map[string]any{"files": files})
}
