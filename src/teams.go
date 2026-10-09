package main

// Teams are domain policy, independent of the authentication provider. Identity
// still comes exclusively from Better Auth; ordinary clients never supply roles.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const teamRegistry = "mist-teams"

var teamIDPattern = regexp.MustCompile(`^team-[a-f0-9]{16}$`)

type TeamPolicy struct {
	CPU             string   `json:"cpu"`
	Memory          string   `json:"memory"`
	NVIDIA          int      `json:"nvidia"`
	Tenstorrent     int      `json:"tenstorrent"`
	Concurrent      int      `json:"concurrent"`
	Queued          int      `json:"queued"`
	StorageGiB      int64    `json:"storage_gib"` // Dataset pool; retained API field for compatibility.
	ModelStorageGiB int64    `json:"model_storage_gib"`
	RuntimeSeconds  int64    `json:"runtime_seconds"`
	Registries      []string `json:"registries"`
}
type TeamMember struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	CommonWriter bool   `json:"common_writer"`
}
type StorageGrant struct {
	ID         string `json:"id"`
	TargetTeam string `json:"target_team"`
	Scope      string `json:"scope"`
}
type Team struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Created  time.Time      `json:"created"`
	Disabled bool           `json:"disabled"`
	Policy   TeamPolicy     `json:"policy"`
	Members  []TeamMember   `json:"members"`
	Grants   []StorageGrant `json:"grants"`
}
type TeamService struct {
	app         *App
	mu          sync.Mutex // admissions and policy changes are serialized on the single controller
	storageMu   sync.Mutex
	stores      map[string]*SharedStorage
	provisioned map[string]teamProvisioned
	stop        chan struct{}
	done        chan struct{}
}
type teamProvisioned struct {
	signature string
	checked   time.Time
}
type teamContextKey struct{}
type TeamAccess struct {
	Team    *Team
	Member  *Member
	Store   *SharedStorage
	Service *TeamService
}

func isAdmin(m *Member) bool { return m != nil && contains(strings.Split(m.Role, ","), "admin") }
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (t *Team) member(id string) *TeamMember {
	for i := range t.Members {
		if t.Members[i].ID == id {
			return &t.Members[i]
		}
	}
	return nil
}
func (t *Team) namespace() string { return "mist-" + t.ID }
func defaultTeamPolicy() TeamPolicy {
	return TeamPolicy{CPU: "8", Memory: "32Gi", NVIDIA: 2, Tenstorrent: 4, Concurrent: 2, Queued: 50, StorageGiB: 200, ModelStorageGiB: 500, RuntimeSeconds: 86400, Registries: []string{"docker.io", "ghcr.io", "nvcr.io"}}
}
func teamStorageBudgetGiB() int64 {
	raw := envOr("MIST_TEAM_STORAGE_BUDGET_GIB", "1500")
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 2 || value > maxDatasetBytes/(1024*1024*1024) {
		panic("invalid MIST_TEAM_STORAGE_BUDGET_GIB")
	}
	return value
}
func (s *TeamService) storageBudgetGiB() int64 {
	budget := teamStorageBudgetGiB()
	if envOr("MIST_REQUIRE_TEAM_FILESYSTEM", "true") == "false" {
		return budget
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(s.app.executor.storage.root, &fs) != nil {
		return 0
	}
	return max(0, min(budget, int64(fs.Blocks)*fs.Bsize/(1024*1024*1024)-20))
}
func normalizeStoragePolicy(p *TeamPolicy) {
	// Old teams had one shared pool. Preserve their allocation for each bucket;
	// the host migration moves completed outputs into the new model filesystem.
	if p.ModelStorageGiB == 0 {
		p.ModelStorageGiB = p.StorageGiB
	}
}
func validateTeam(team *Team) error {
	normalizeStoragePolicy(&team.Policy)
	if strings.TrimSpace(team.Name) == "" || len(team.Name) > 100 {
		return errors.New("team name must contain 1–100 characters")
	}
	p := team.Policy
	cpu, err := resource.ParseQuantity(p.CPU)
	if err != nil || cpu.Sign() <= 0 || cpu.Cmp(resource.MustParse("32")) > 0 {
		return errors.New("team CPU must be greater than zero and at most 32")
	}
	mem, err := resource.ParseQuantity(p.Memory)
	if err != nil || mem.Cmp(resource.MustParse("64Mi")) < 0 || mem.Cmp(resource.MustParse("128Gi")) > 0 {
		return errors.New("team memory must be between 64Mi and 128Gi")
	}
	if p.NVIDIA < 0 || p.NVIDIA > 2 || p.Tenstorrent < 0 || p.Tenstorrent > 4 || p.Concurrent < 1 || p.Concurrent > 16 || p.Queued < 1 || p.Queued > 200 || p.StorageGiB < 1 || p.StorageGiB > teamStorageBudgetGiB() || p.ModelStorageGiB < 1 || p.ModelStorageGiB > teamStorageBudgetGiB() || p.RuntimeSeconds < 10 || p.RuntimeSeconds > 86400 {
		return errors.New("invalid team device, concurrency, queue, storage or runtime limits")
	}
	if len(p.Registries) > 20 {
		return errors.New("too many image registries")
	}
	for _, host := range p.Registries {
		if !registryPattern.MatchString(host) || strings.Contains(host, "..") {
			return errors.New("image registries must be lowercase public registry hostnames")
		}
	}
	return nil
}
func newTeamService(a *App) *TeamService {
	_ = teamStorageBudgetGiB()
	return &TeamService{app: a, stores: map[string]*SharedStorage{}, provisioned: map[string]teamProvisioned{}, stop: make(chan struct{}), done: make(chan struct{})}
}
func (s *TeamService) registry(ctx context.Context) (*corev1.ConfigMap, []Team, error) {
	cm, err := s.app.executor.client.CoreV1().ConfigMaps("mist-system").Get(ctx, teamRegistry, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: teamRegistry, Namespace: "mist-system"}, Data: map[string]string{"teams": "[]"}}, []Team{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var teams []Team
	if err = json.Unmarshal([]byte(cm.Data["teams"]), &teams); err != nil {
		return nil, nil, fmt.Errorf("invalid persisted team registry: %w", err)
	}
	for i := range teams {
		normalizeStoragePolicy(&teams[i].Policy)
	}
	return cm, teams, nil
}
func (s *TeamService) mutate(ctx context.Context, change func(*[]Team) error) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, teams, err := s.registry(ctx)
		if err != nil {
			return err
		}
		if err = change(&teams); err != nil {
			return &submissionError{err}
		}
		var budget int64
		for _, t := range teams {
			budget += t.Policy.StorageGiB + t.Policy.ModelStorageGiB
		}
		if budget > s.storageBudgetGiB() {
			return &submissionError{fmt.Errorf("total dataset and model allocations cannot exceed the %dGiB storage budget", s.storageBudgetGiB())}
		}
		data, err := json.Marshal(teams)
		if err != nil {
			return err
		}
		if len(data) > 240*1024 {
			return &submissionError{errors.New("team registry is full")}
		}
		cm.Data["teams"] = string(data)
		if cm.ResourceVersion == "" {
			_, err = s.app.executor.client.CoreV1().ConfigMaps("mist-system").Create(ctx, cm, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				return apierrors.NewConflict(corev1.Resource("configmaps"), teamRegistry, err)
			}
		} else {
			_, err = s.app.executor.client.CoreV1().ConfigMaps("mist-system").Update(ctx, cm, metav1.UpdateOptions{})
		}
		return err
	})
}
func findTeam(teams []Team, id string) *Team {
	for i := range teams {
		if teams[i].ID == id {
			return &teams[i]
		}
	}
	return nil
}
func (s *TeamService) team(ctx context.Context, id string) (*Team, error) {
	_, teams, err := s.registry(ctx)
	if err != nil {
		return nil, err
	}
	t := findTeam(teams, id)
	if t == nil {
		return nil, os.ErrNotExist
	}
	return t, nil
}
func (s *TeamService) store(t *Team) (*SharedStorage, error) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	status, err := os.ReadFile(filepath.Join(s.app.executor.storage.root, "metadata/provision-status", t.ID+".json"))
	if err != nil {
		return nil, errors.New("team storage is being provisioned")
	}
	var ready struct {
		State           string `json:"state"`
		StorageGiB      int64  `json:"storage_gib"`
		ModelStorageGiB int64  `json:"model_storage_gib"`
	}
	if json.Unmarshal(status, &ready) != nil || ready.State != "Ready" || ready.StorageGiB < t.Policy.StorageGiB || ready.ModelStorageGiB < t.Policy.ModelStorageGiB {
		return nil, errors.New("team storage is not ready")
	}
	if envOr("MIST_REQUIRE_TEAM_FILESYSTEM", "true") != "false" {
		var datasetDevice uint64
		for index, pool := range []struct {
			path string
			gib  int64
		}{{filepath.Join(s.app.executor.storage.root, "teams", t.ID), t.Policy.StorageGiB}, {filepath.Join(s.app.executor.storage.root, "teams", t.ID, ".models"), t.Policy.ModelStorageGiB}} {
			var fs syscall.Statfs_t
			if err := syscall.Statfs(pool.path, &fs); err != nil {
				return nil, err
			}
			// NFS reports a zero statfs fsid on these nodes. The device ID
			// identifies distinct mounted superblocks, including NFS child mounts.
			entry, err := os.Stat(pool.path)
			if err != nil {
				return nil, err
			}
			identity, ok := entry.Sys().(*syscall.Stat_t)
			if !ok {
				return nil, errors.New("cannot identify team filesystem")
			}
			if index == 0 {
				datasetDevice = uint64(identity.Dev)
			} else if uint64(identity.Dev) == datasetDevice {
				return nil, errors.New("separate model filesystem is unavailable")
			}
			capacity := fs.Blocks * uint64(fs.Bsize)
			target := uint64(pool.gib) * 1024 * 1024 * 1024
			if capacity > target+64*1024*1024 || capacity < target*9/10 {
				return nil, errors.New("bounded team filesystem is unavailable")
			}
		}
	}
	if st := s.stores[t.ID]; st != nil {
		return st, nil
	}
	st, err := NewSharedStorage(filepath.Join(s.app.executor.storage.root, "teams", t.ID))
	if err != nil {
		return nil, err
	}
	st.modelRoot = filepath.Join(st.root, ".models")
	if envOr("MIST_REQUIRE_TEAM_FILESYSTEM", "true") == "false" {
		if err := os.MkdirAll(st.modelRoot, 0700); err != nil {
			return nil, err
		}
	}
	s.stores[t.ID] = st
	return st, nil
}
func (s *TeamService) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Mist-Team")
		if id == "" {
			id = r.URL.Query().Get("team_id")
		}
		if id == "" || strings.HasPrefix(r.URL.Path, "/teams") || r.URL.Path == "/session" || r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/auth/") {
			if id == "" && r.Method == http.MethodPost && (r.URL.Path == "/jobs" || r.URL.Path == "/datasets") {
				writeJSON(w, 403, map[string]string{"error": "Select a team workspace before submitting jobs or uploading files"})
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		m, _ := r.Context().Value(memberKey{}).(*Member)
		if m == nil {
			writeJSON(w, 401, map[string]string{"error": "Sign in to Mist"})
			return
		}
		t, err := s.team(r.Context(), id)
		if err != nil {
			if !os.IsNotExist(err) {
				executorError(w, err)
			} else {
				writeJSON(w, 404, map[string]string{"error": "team not found"})
			}
			return
		}
		if t.Disabled || t.member(m.ID) == nil {
			writeJSON(w, 403, map[string]string{"error": "You are not an active member of this team"})
			return
		}
		store, err := s.store(t)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": err.Error()})
			return
		}
		access := &TeamAccess{Team: t, Member: m, Store: store, Service: s}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), teamContextKey{}, access)))
	})
}
func randomID(prefix string) (string, error) {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	return prefix + hex.EncodeToString(b), err
}
func decodeTeamJSON(w http.ResponseWriter, r *http.Request, value any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return &submissionError{err}
	}
	if d.Decode(new(any)) != io.EOF {
		return &submissionError{errors.New("expected one JSON object")}
	}
	return nil
}
func (a *App) teamEndpoint(w http.ResponseWriter, r *http.Request) {
	s := a.teams
	if s == nil {
		writeJSON(w, 503, map[string]string{"error": "Team workspaces are not enabled"})
		return
	}
	m, _ := r.Context().Value(memberKey{}).(*Member)
	if r.URL.Path == "/teams" && r.Method == http.MethodGet {
		_, teams, err := s.registry(r.Context())
		if err != nil {
			executorError(w, err)
			return
		}
		result := []Team{}
		for _, t := range teams {
			if isAdmin(m) || t.member(m.ID) != nil {
				result = append(result, t)
			}
		}
		writeJSON(w, 200, map[string]any{"teams": result, "storage_budget_gib": s.storageBudgetGiB()})
		return
	}
	if !isAdmin(m) {
		writeJSON(w, 403, map[string]string{"error": "Administrator access required"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/teams"), "/")
	if r.URL.Path == "/teams" && r.Method == http.MethodPost {
		var body struct {
			Name            string      `json:"name"`
			StorageGiB      *int64      `json:"storage_gib"`
			ModelStorageGiB *int64      `json:"model_storage_gib"`
			Policy          *TeamPolicy `json:"policy"`
		}
		if err := decodeTeamJSON(w, r, &body); err != nil {
			executorError(w, err)
			return
		}
		id, err := randomID("team-")
		if err != nil {
			executorError(w, err)
			return
		}
		t := Team{ID: id, Name: body.Name, Created: time.Now().UTC(), Policy: defaultTeamPolicy(), Members: []TeamMember{}, Grants: []StorageGrant{}}
		if body.Policy != nil {
			t.Policy = *body.Policy
		}
		if body.StorageGiB != nil {
			t.Policy.StorageGiB = *body.StorageGiB
			if body.ModelStorageGiB == nil {
				t.Policy.ModelStorageGiB = *body.StorageGiB
			}
		}
		if body.ModelStorageGiB != nil {
			if *body.ModelStorageGiB < 1 {
				executorError(w, &submissionError{errors.New("model storage must be at least 1 GiB")})
				return
			}
			t.Policy.ModelStorageGiB = *body.ModelStorageGiB
		}
		if err = validateTeam(&t); err != nil {
			executorError(w, &submissionError{err})
			return
		}
		err = s.mutate(r.Context(), func(list *[]Team) error {
			if len(*list) >= 100 {
				return errors.New("at most 100 teams are supported")
			}
			*list = append(*list, t)
			return nil
		})
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 201, t)
		return
	}
	if len(parts) < 2 || !teamIDPattern.MatchString(parts[1]) {
		writeJSON(w, 404, map[string]string{"error": "team not found"})
		return
	}
	id := parts[1]
	switch {
	case len(parts) == 2 && r.Method == http.MethodPatch:
		var body struct {
			Name     *string     `json:"name"`
			Policy   *TeamPolicy `json:"policy"`
			Disabled *bool       `json:"disabled"`
		}
		if err := decodeTeamJSON(w, r, &body); err != nil {
			executorError(w, err)
			return
		}
		err := s.mutate(r.Context(), func(list *[]Team) error {
			t := findTeam(*list, id)
			if t == nil {
				return errors.New("team not found")
			}
			if body.Name != nil {
				t.Name = *body.Name
			}
			if body.Disabled != nil {
				t.Disabled = *body.Disabled
			}
			if body.Policy != nil {
				normalizeStoragePolicy(body.Policy)
				if body.Policy.StorageGiB < t.Policy.StorageGiB || body.Policy.ModelStorageGiB < t.Policy.ModelStorageGiB {
					return errors.New("storage cannot be shrunk online; existing files are preserved")
				}
				t.Policy = *body.Policy
			}
			return validateTeam(t)
		})
		if err != nil {
			executorError(w, err)
			return
		}
	case len(parts) == 3 && parts[2] == "members" && r.Method == http.MethodPost:
		var body struct {
			UserID       string `json:"user_id"`
			CommonWriter bool   `json:"common_writer"`
		}
		if err := decodeTeamJSON(w, r, &body); err != nil {
			executorError(w, err)
			return
		}
		member, err := a.auth.lookupMember(r, body.UserID)
		if err != nil {
			executorError(w, &submissionError{errors.New("select an existing active Mist account")})
			return
		}
		err = s.mutate(r.Context(), func(list *[]Team) error {
			t := findTeam(*list, id)
			if t == nil {
				return errors.New("team not found")
			}
			entry := TeamMember{ID: member.ID, Name: member.Name, Email: member.Email, CommonWriter: body.CommonWriter}
			if old := t.member(member.ID); old != nil {
				*old = entry
			} else {
				t.Members = append(t.Members, entry)
			}
			return nil
		})
		if err != nil {
			executorError(w, err)
			return
		}
	case len(parts) == 4 && parts[2] == "members" && r.Method == http.MethodDelete:
		err := s.mutate(r.Context(), func(list *[]Team) error {
			t := findTeam(*list, id)
			if t == nil {
				return errors.New("team not found")
			}
			members := []TeamMember{}
			for _, member := range t.Members {
				if member.ID != parts[3] {
					members = append(members, member)
				}
			}
			t.Members = members
			return nil
		})
		if err != nil {
			executorError(w, err)
			return
		}
	case len(parts) == 3 && parts[2] == "grants" && r.Method == http.MethodPost:
		var body struct {
			TargetTeam string `json:"target_team"`
			Scope      string `json:"scope"`
		}
		if err := decodeTeamJSON(w, r, &body); err != nil {
			executorError(w, err)
			return
		}
		grantID, err := randomID("grant-")
		if err != nil {
			executorError(w, err)
			return
		}
		err = s.mutate(r.Context(), func(list *[]Team) error {
			t := findTeam(*list, id)
			target := findTeam(*list, body.TargetTeam)
			if t == nil || target == nil || target.Disabled || target.ID == id {
				return errors.New("select a different active target team")
			}
			if body.Scope != "common" && t.member(body.Scope) == nil {
				return errors.New("scope must be common or a source team member ID")
			}
			for _, g := range t.Grants {
				if g.TargetTeam == body.TargetTeam && g.Scope == body.Scope {
					return errors.New("this grant already exists")
				}
			}
			t.Grants = append(t.Grants, StorageGrant{ID: grantID, TargetTeam: body.TargetTeam, Scope: body.Scope})
			return nil
		})
		if err != nil {
			executorError(w, err)
			return
		}
	case len(parts) == 4 && parts[2] == "grants" && r.Method == http.MethodDelete:
		err := s.mutate(r.Context(), func(list *[]Team) error {
			t := findTeam(*list, id)
			if t == nil {
				return errors.New("team not found")
			}
			grants := []StorageGrant{}
			for _, g := range t.Grants {
				if g.ID != parts[3] {
					grants = append(grants, g)
				}
			}
			t.Grants = grants
			return nil
		})
		if err != nil {
			executorError(w, err)
			return
		}
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	// Reconcile revoked job access before reporting successful policy changes.
	if err := s.revokeJobs(r.Context()); err != nil {
		writeJSON(w, 503, map[string]string{"error": "Policy saved; job access reconciliation pending: " + err.Error()})
		return
	}
	t, err := s.team(r.Context(), id)
	if err != nil {
		executorError(w, err)
		return
	}
	writeJSON(w, 200, t)
}
func (g *AuthGateway) lookupMember(r *http.Request, id string) (*Member, error) {
	u := *g.upstream
	u.Path = "/auth/admin/get-user"
	q := u.Query()
	q.Set("id", id)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", r.Header.Get("Cookie"))
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("account unavailable")
	}
	var member Member
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&member); err != nil {
		return nil, err
	}
	if member.ID != id || member.Banned {
		return nil, errors.New("account unavailable")
	}
	return &member, nil
}
func sortedTeams(teams []Team) {
	sort.Slice(teams, func(i, j int) bool { return teams[i].ID < teams[j].ID })
}
