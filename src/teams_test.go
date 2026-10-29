package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func departmentTestApp(t *testing.T) (*App, *fake.Clientset, []Team) {
	t.Helper()
	root := t.TempDir()
	store, err := NewSharedStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	teams := []Team{{ID: "team-aaaaaaaaaaaaaaaa", Name: "A", Policy: defaultTeamPolicy(), Members: []TeamMember{{ID: "alice", Name: "Alice"}}, Grants: []StorageGrant{}}, {ID: "team-bbbbbbbbbbbbbbbb", Name: "B", Policy: defaultTeamPolicy(), Members: []TeamMember{{ID: "bob", Name: "Bob"}}, Grants: []StorageGrant{}}}
	data, _ := json.Marshal(teams)
	c := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: teamRegistry, Namespace: "mist-system", ResourceVersion: "1"}, Data: map[string]string{"teams": string(data)}}, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "training-smoke-script", Namespace: "mist"}, Data: map[string]string{"training_smoke.py": "print('ok')"}}, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "main"}, Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8"), corev1.ResourceMemory: resource.MustParse("32Gi")}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}})
	c.PrependReactor("create", "configmaps", func(action kt.Action) (bool, runtime.Object, error) {
		action.(kt.CreateAction).GetObject().(*corev1.ConfigMap).ResourceVersion = "1"
		return false, nil, nil
	})
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("mist-tenstorrent-%d-boards", i)
		_, err = c.ResourceV1().ResourceClaimTemplates("mist").Create(context.Background(), &resourcev1.ResourceClaimTemplate{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MIST_TEAMS_ENABLED", "true")
	t.Setenv("MIST_AUTH_DATABASE", filepath.Join(t.TempDir(), "auth.sqlite"))
	e := &KubernetesExecutor{client: c, namespace: "mist", owner: "legacy", storage: store, cpuNode: "main", ttNode: "tt", allowedImages: map[string]bool{cpuImage: true, ttImage: true, nvidiaImage: true}}
	app := NewKubernetesApp(e, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, m := range []Member{
		{ID: "alice", Name: "Alice", Email: "alice@mist.local", Role: "user"},
		{ID: "bob", Name: "Bob", Email: "bob@mist.local", Role: "user"},
		{ID: "admin", Name: "Admin", Email: "admin@mist.local", Role: "admin"},
	} {
		if _, err = app.auth.db.Exec(`
			INSERT INTO user (id, name, email, role, createdAt, updatedAt)
			VALUES (?, ?, ?, ?, ?, ?)
		`, m.ID, m.Name, m.Email, m.Role, now, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"alice", "bob", "admin"} {
		seedTestSession(t, app.auth, id)
	}
	t.Cleanup(func() { _ = app.auth.Close() })

	for _, team := range teams {
		for _, pool := range []string{"datasets", "results"} {
			if err := os.MkdirAll(filepath.Join(root, "teams", team.ID, pool), 0750); err != nil {
				t.Fatal(err)
			}
		}
		if err = atomicJSON(filepath.Join(root, "metadata/provision-status", team.ID+".json"), teamStorageStatus{State: "Ready", Layout: sharedFolderLayout, UsageObservedUnix: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
		if _, err = app.teams.store(&team); err != nil {
			t.Fatal(err)
		}
	}
	return app, c, teams
}
func departmentRequest(a *App, method, path, identity, team, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Cookie", "better-auth.session_token=test-session-"+identity)
	if team != "" {
		req.Header.Set("X-Mist-Team", team)
	}
	w := httptest.NewRecorder()
	a.httpServer.Handler.ServeHTTP(w, req)
	return w
}
func TestDepartmentTeamPermissionsAndScopedSharing(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	alice, bob := teams[0], teams[1]
	r := departmentRequest(a, "POST", "/datasets?filename=data.txt&scope=common", "alice", alice.ID, "data")
	if r.Code != 403 {
		t.Fatalf("member wrote common folder: %d %s", r.Code, r.Body)
	}
	r = departmentRequest(a, "POST", "/datasets?filename=data.txt", "alice", alice.ID, "data")
	if r.Code != 201 {
		t.Fatalf("own upload: %d %s", r.Code, r.Body)
	}
	var ds Dataset
	_ = json.Unmarshal(r.Body.Bytes(), &ds)
	if ds.Scope != "alice" || !strings.HasPrefix(ds.ContentPath, "members/") {
		t.Fatalf("incorrect member folder: %+v", ds)
	}
	for _, path := range []string{"/datasets/" + ds.ID, "/datasets/" + ds.ID + "/files", "/storage/files?source_team=" + alice.ID + "&scope=alice"} {
		r = departmentRequest(a, "GET", path, "bob", bob.ID, "")
		if r.Code != 404 {
			t.Fatalf("foreign data exposed at %s: %d", path, r.Code)
		}
	}
	if r = departmentRequest(a, "GET", "/datasets", "bob", alice.ID, ""); r.Code != 403 {
		t.Fatal("spoofed team membership accepted")
	}
	if r = departmentRequest(a, "POST", "/teams", "bob", "", `{"name":"bad"}`); r.Code != 403 {
		t.Fatal("member created team")
	}
	if r = departmentRequest(a, "POST", "/jobs", "alice", "", `{"command":["true"]}`); r.Code != 403 {
		t.Fatal("legacy submission bypasses team limits")
	}
	r = departmentRequest(a, "POST", "/teams/"+alice.ID+"/grants", "admin", "", `{"target_team":"`+bob.ID+`","scope":"alice"}`)
	if r.Code != 200 {
		t.Fatalf("grant failed: %d %s", r.Code, r.Body)
	}
	r = departmentRequest(a, "GET", "/datasets/"+ds.ID+"/files", "bob", bob.ID, "")
	if r.Code != 200 || !strings.Contains(r.Body.String(), "data.txt") {
		t.Fatalf("grant did not expose files: %d %s", r.Code, r.Body)
	}
	r = departmentRequest(a, "DELETE", "/datasets/"+ds.ID, "bob", bob.ID, "")
	if r.Code != 403 {
		t.Fatal("read grant permits deletion")
	}
	r = departmentRequest(a, "POST", "/jobs", "bob", bob.ID, `{"command":["true"],"dataset_id":"`+ds.ID+`"}`)
	if r.Code != 201 {
		t.Fatalf("shared input submission: %d %s", r.Code, r.Body)
	}
	var response struct {
		Job KubernetesJobStatus `json:"job"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &response)
	if response.Job.StorageScope != "bob" {
		t.Fatal("output ownership incorrectly inherited from input")
	}
	_, updated, _ := a.teams.registry(context.Background())
	grant := findTeam(updated, alice.ID).Grants[0]
	r = departmentRequest(a, "DELETE", "/teams/"+alice.ID+"/grants/"+grant.ID, "admin", "", "")
	if r.Code != 200 {
		t.Fatalf("revoke failed: %d %s", r.Code, r.Body)
	}
	r = departmentRequest(a, "GET", "/jobs/"+response.Job.ID, "bob", bob.ID, "")
	if !strings.Contains(r.Body.String(), `"job_state":"Cancelled"`) {
		t.Fatalf("revoked queued job not stopped: %s", r.Body)
	}
	r = departmentRequest(a, "GET", "/datasets/"+ds.ID+"/files", "bob", bob.ID, "")
	if r.Code != 404 {
		t.Fatal("revoked grant remains effective")
	}
}
func TestDepartmentPolicyAndAdvisoryStorage(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	p := defaultTeamPolicy()
	p.CPU = "1"
	p.NVIDIA = 0
	p.RuntimeSeconds = 60
	team := teams[0]
	team.Policy = p
	access := &TeamAccess{Team: &team, Member: &Member{ID: "alice"}}
	e := *a.executor
	e.team = access
	for _, req := range []CreateJobRequest{{Command: []string{"true"}, CPU: "2"}, {Command: []string{"true"}, Accelerator: "nvidia"}, {Command: []string{"true"}, StorageScope: "bob"}, {Command: []string{"true"}, Image: "evil.invalid/test:v1"}, {Command: []string{"true"}, Image: "ghcr.io/team/app"}} {
		req.TimeoutSeconds = 30
		if _, err := e.normalize(req); err == nil {
			t.Fatalf("accepted invalid team request: %+v", req)
		}
	}
	if _, err := e.normalize(CreateJobRequest{Command: []string{"true"}, TimeoutSeconds: 30}); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := a.teams.mutate(context.Background(), func(list *[]Team) error { (*list)[0].Policy.StorageGiB = 1500; return nil }); err != nil {
		t.Fatalf("advisory allowance must not reserve disk space: %v", err)
	}
	changed := departmentRequest(a, "GET", "/teams", "alice", "", "")
	if changed.Code != 200 || !strings.Contains(changed.Body.String(), `"storage_limits_enforced":false`) {
		t.Fatalf("advisory semantics missing: %s", changed.Body)
	}
}
func TestDepartmentDurableFairAdmission(t *testing.T) {
	a, c, teams := departmentTestApp(t)
	ctx := context.Background()
	for i := range teams {
		teams[i].Policy.Concurrent = 1
	}
	_ = a.teams.mutate(ctx, func(list *[]Team) error { *list = teams; return nil })
	ids := map[string][]string{}
	for _, team := range teams {
		store, _ := a.teams.store(&team)
		for i := 0; i < 2; i++ {
			e := *a.executor
			e.namespace, e.owner, e.storage, e.sharedPVC = team.namespace(), team.ID, store, "team-datasets"
			e.team = &TeamAccess{Team: &team, Member: &Member{ID: team.Members[0].ID}, Service: a.teams}
			job, err := e.submit(ctx, CreateJobRequest{Command: []string{"true"}, CPU: "1", Memory: "64Mi"})
			if err != nil {
				t.Fatal(err)
			}
			ids[team.ID] = append(ids[team.ID], job.ID)
		}
	}
	if err := a.teams.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	for _, team := range teams {
		for i, id := range ids[team.ID] {
			job, err := c.BatchV1().Jobs(team.namespace()).Get(ctx, id, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if (job.Annotations["mist.io/admitted-at"] != "") != (i == 0) {
				t.Fatalf("fair admission/concurrency failed for %s job %d", team.ID, i)
			}
		}
	}
	// Reconstruct controller state as after a restart; reservations remain in Jobs.
	restarted := newTeamService(a)
	if err := restarted.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	for _, team := range teams {
		job, _ := c.BatchV1().Jobs(team.namespace()).Get(ctx, ids[team.ID][1], metav1.GetOptions{})
		if job.Spec.Suspend == nil || !*job.Spec.Suspend {
			t.Fatal("restart lost active reservations")
		}
		first, _ := c.BatchV1().Jobs(team.namespace()).Get(ctx, ids[team.ID][0], metav1.GetOptions{})
		first.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(time.Now())}}
		_, _ = c.BatchV1().Jobs(team.namespace()).UpdateStatus(ctx, first, metav1.UpdateOptions{})
	}
	if err := restarted.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	for _, team := range teams {
		job, _ := c.BatchV1().Jobs(team.namespace()).Get(ctx, ids[team.ID][1], metav1.GetOptions{})
		if job.Spec.Suspend == nil || *job.Spec.Suspend {
			t.Fatal("completed jobs did not release admission")
		}
	}
}

func TestDepartmentAgedHeadDrainsCapacityWithoutStarvation(t *testing.T) {
	a, c, teams := departmentTestApp(t)
	ctx := context.Background()
	create := func(index int, id string, cpu string, admitted bool, age time.Duration) *batchv1.Job {
		team := teams[index]
		e := *a.executor
		e.owner, e.namespace = team.ID, team.namespace()
		e.team = &TeamAccess{Team: &team, Member: &Member{ID: team.Members[0].ID}}
		req, err := e.normalize(CreateJobRequest{Command: []string{"true"}, CPU: cpu, Memory: "64Mi"})
		if err != nil {
			t.Fatal(err)
		}
		job := e.buildJob(id, req)
		job.Spec.Suspend = ptr(!admitted)
		job.Annotations["mist.io/queued-at"] = time.Now().Add(-age).Format(time.RFC3339Nano)
		if admitted {
			job.Annotations["mist.io/admitted-at"] = time.Now().Format(time.RFC3339Nano)
		}
		saved, err := c.BatchV1().Jobs(e.namespace).Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	big := create(0, "mist-aaaaaaaaaaaaaaaaaaaaaaaa", "8", false, 40*time.Second)
	running := create(1, "mist-bbbbbbbbbbbbbbbbbbbbbbbb", "4", true, time.Minute)
	small := create(1, "mist-cccccccccccccccccccccccc", "2", false, 0)
	if err := a.teams.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	still, _ := c.BatchV1().Jobs(small.Namespace).Get(ctx, small.Name, metav1.GetOptions{})
	if still.Spec.Suspend == nil || !*still.Spec.Suspend {
		t.Fatal("small requests overtook an aged large request")
	}
	running.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	_, _ = c.BatchV1().Jobs(running.Namespace).UpdateStatus(ctx, running, metav1.UpdateOptions{})
	if err := a.teams.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	admitted, _ := c.BatchV1().Jobs(big.Namespace).Get(ctx, big.Name, metav1.GetOptions{})
	if admitted.Spec.Suspend == nil || *admitted.Spec.Suspend {
		t.Fatal("large request did not receive drained capacity")
	}
}
func TestDepartmentDemotionRevokesAdministratorWritePrivilege(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	team := teams[0]
	e := *a.executor
	e.team = &TeamAccess{Team: &team, Member: &Member{ID: "alice", Role: "admin"}}
	req, err := e.normalize(CreateJobRequest{Command: []string{"true"}, StorageScope: "common"})
	if err != nil {
		t.Fatal(err)
	}
	job := e.buildJob("mist-dddddddddddddddddddddddd", req)
	reason, err := a.teams.revoked(context.Background(), &team, job, map[string]AccountStatus{"alice": {Role: "user"}})
	if err != nil || reason == "" {
		t.Fatalf("historical admin flag bypassed current role: %s %v", reason, err)
	}
}
func TestMemberProxyAddressCannotBeSpoofed(t *testing.T) {
	t.Setenv("MIST_TRUSTED_PROXY_IPS", "10.42.0.1,10.0.0.175")
	r := httptest.NewRequest("GET", "/auth/get-session", nil)
	r.RemoteAddr = "100.127.1.1:12345"
	r.Header.Set("X-Real-IP", "1.2.3.4")
	if clientAddress(r) != "100.127.1.1" {
		t.Fatal("untrusted caller spoofed rate-limit address")
	}
	r.RemoteAddr = "10.42.0.1:23456"
	if clientAddress(r) != "1.2.3.4" {
		t.Fatal("trusted Nginx address not forwarded")
	}
	r.Header.Set("X-Real-IP", "1.2.3.4,5.6.7.8")
	if clientAddress(r) != "10.42.0.1" {
		t.Fatal("forwarded chain accepted")
	}
}

// A claim can be owned by its Pod before the Pod is bound. The device inventory
// already subtracts that allocation; admission must not reserve it a second time.
func TestDepartmentPendingPodClaimDoesNotDoubleReserveBoards(t *testing.T) {
	a, c, teams := departmentTestApp(t)
	ctx := context.Background()
	nodes, pods, slices := hardwareFixture()
	a.executor.cpuNode, a.executor.ttNode = "cpu-node", "tt-node"
	for _, node := range nodes {
		node.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("32")
		node.Status.Allocatable[corev1.ResourceMemory] = resource.MustParse("128Gi")
		if _, err := c.CoreV1().Nodes().Create(ctx, &node, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range pods {
		pods[i].Name = fmt.Sprintf("driver-%d", i)
		if _, err := c.CoreV1().Pods("drivers").Create(ctx, &pods[i], metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	slices[0].Name = "boards"
	slices[0].Spec.Devices = slices[0].Spec.Devices[:2]
	if _, err := c.ResourceV1().ResourceSlices().Create(ctx, &slices[0], metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	team := teams[0]
	request := CreateJobRequest{Accelerator: "tenstorrent", DeviceCount: 1, CPU: "2", Memory: "4Gi"}
	data, _ := json.Marshal(request)
	for _, name := range []string{"pending-allocated", "queued"} {
		annotations := map[string]string{requestAnnotation: string(data), "mist.io/queued-at": time.Now().Format(time.RFC3339Nano)}
		if name == "pending-allocated" {
			annotations["mist.io/admitted-at"] = time.Now().Format(time.RFC3339Nano)
		}
		_, err := c.BatchV1().Jobs(team.namespace()).Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"mist.io/team": team.ID}, Annotations: annotations}, Spec: batchv1.JobSpec{Suspend: ptr(name == "queued")}}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := c.CoreV1().Pods(team.namespace()).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unbound", Labels: map[string]string{"batch.kubernetes.io/job-name": "pending-allocated"}}, Status: corev1.PodStatus{Phase: corev1.PodPending}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claim := resourcev1.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Name: "allocated", OwnerReferences: []metav1.OwnerReference{{Kind: "Pod", Name: "unbound"}}}, Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{{Driver: "tenstorrent.com", Pool: "tt-node", Device: "tt-0"}}}}}}
	if _, err = c.ResourceV1().ResourceClaims(team.namespace()).Create(ctx, &claim, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = a.teams.admit(ctx, teams); err != nil {
		t.Fatal(err)
	}
	queued, err := c.BatchV1().Jobs(team.namespace()).Get(ctx, "queued", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Annotations["mist.io/admitted-at"] == "" {
		t.Fatal("free board withheld by double-counting pending Pod claim")
	}
}

func TestDepartmentSplitDatasetAndModelPaths(t *testing.T) {
	a, c, teams := departmentTestApp(t)
	team := teams[0]
	uploaded := departmentRequest(a, "POST", "/datasets?filename=train.csv", "alice", team.ID, "1,2\n")
	if uploaded.Code != 201 {
		t.Fatal(uploaded.Body.String())
	}
	var ds Dataset
	json.Unmarshal(uploaded.Body.Bytes(), &ds)
	submitted := departmentRequest(a, "POST", "/jobs", "alice", team.ID, fmt.Sprintf(`{"accelerator":"cpu","type":"command","command":["true"],"dataset_id":%q}`, ds.ID))
	if submitted.Code != 201 {
		t.Fatal(submitted.Body.String())
	}
	var result struct {
		ID string `json:"job_id"`
	}
	json.Unmarshal(submitted.Body.Bytes(), &result)
	job, err := c.BatchV1().Jobs(team.namespace()).Get(context.Background(), result.ID, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]string{}
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			claims[volume.Name] = volume.PersistentVolumeClaim.ClaimName
		}
	}
	if claims["input"] != "team-datasets" || claims["checkpoints"] != "team-results" {
		t.Fatalf("storage pools not separated: %+v", claims)
	}
	store, err := a.teams.store(&team)
	if err != nil {
		t.Fatal(err)
	}
	output, err := store.outputPath(job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output, store.modelRoot+string(os.PathSeparator)) {
		t.Fatal("result path falls into datasets")
	}
	if err = os.WriteFile(filepath.Join(output, "weights.bin"), []byte("WEIGHTS"), 0600); err != nil {
		t.Fatal(err)
	}
	listed := departmentRequest(a, "GET", "/storage/files?scope=alice", "alice", team.ID, "")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), "weights.bin") || !strings.Contains(listed.Body.String(), "train.csv") {
		t.Fatalf("virtual scope lost one pool: %s", listed.Body.String())
	}
	path := "/storage/files?scope=alice&download=true&path=jobs/" + job.Name + "/outputs/weights.bin"
	downloaded := departmentRequest(a, "GET", path, "alice", team.ID, "")
	if downloaded.Code != 200 || downloaded.Body.String() != "WEIGHTS" {
		t.Fatal("model download failed")
	}
	escaped := departmentRequest(a, "GET", path+"/../../../../outside", "alice", team.ID, "")
	if escaped.Code == 200 {
		t.Fatal("virtual model folder escaped its scope")
	}
}

func TestDepartmentFoldersFailClosedForMissingOrUnsafePaths(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	team := teams[0]
	results := filepath.Join(a.executor.storage.root, "teams", team.ID, "results")
	if err := os.RemoveAll(results); err != nil {
		t.Fatal(err)
	}
	if _, err := a.teams.store(&team); err == nil {
		t.Fatal("missing results directory accepted")
	}
	if err := os.Symlink(t.TempDir(), results); err != nil {
		t.Fatal(err)
	}
	if _, err := a.teams.store(&team); err == nil {
		t.Fatal("symlink results directory accepted")
	}
	if err := os.Remove(results); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(results, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.executor.storage.root, "metadata/provision-status", team.ID+".json")
	if err := atomicJSON(path, map[string]any{"state": "Ready", "storage_gib": 200, "model_storage_gib": 500}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.teams.store(&team); err == nil {
		t.Fatal("old image layout status accepted")
	}
}

func TestDepartmentSharedStorageUsageAndAdvisoryDecrease(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	team := teams[0]
	path := filepath.Join(a.executor.storage.root, "metadata/provision-status", team.ID+".json")
	status := teamStorageStatus{State: "Ready", Layout: sharedFolderLayout, DatasetUsedBytes: 123, ModelUsedBytes: 456, UsageObservedUnix: time.Now().Add(-3 * time.Minute).Unix()}
	if err := atomicJSON(path, status); err != nil {
		t.Fatal(err)
	}
	response := departmentRequest(a, "GET", "/storage", "alice", team.ID, "")
	var info struct {
		DatasetUsed    int64  `json:"dataset_used_bytes"`
		ModelUsed      int64  `json:"model_used_bytes"`
		Stale          bool   `json:"usage_stale"`
		Enforced       bool   `json:"storage_limits_enforced"`
		Capacity       uint64 `json:"capacity_bytes"`
		Available      uint64 `json:"available_bytes"`
		ModelCapacity  uint64 `json:"model_capacity_bytes"`
		ModelAvailable uint64 `json:"model_available_bytes"`
	}
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.DatasetUsed != 123 || info.ModelUsed != 456 || !info.Stale || info.Enforced || info.Capacity == 0 || info.Available == 0 {
		t.Fatalf("incorrect folder usage: %+v", info)
	}
	if info.ModelCapacity != info.Capacity || info.ModelAvailable != info.Available {
		t.Fatal("compatibility fields must refer to the same shared disk")
	}
	policy := team.Policy
	policy.StorageGiB, policy.ModelStorageGiB = 1, 1
	body, _ := json.Marshal(map[string]any{"policy": policy})
	changed := departmentRequest(a, "PATCH", "/teams/"+team.ID, "admin", "", string(body))
	if changed.Code != 200 {
		t.Fatalf("allowance decrease must not require migration: %s", changed.Body)
	}
	upload := departmentRequest(a, "POST", "/datasets?filename=tiny.csv", "alice", team.ID, "1,2\n")
	if upload.Code != 201 {
		t.Fatalf("advisory change blocked upload: %s", upload.Body)
	}
}

func TestDepartmentDefaultPoolsAndLegacyPolicy(t *testing.T) {
	a, _, _ := departmentTestApp(t)
	// Leave room for a default team without allocating any real disk images.
	if err := a.teams.mutate(context.Background(), func(teams *[]Team) error {
		for i := range *teams {
			(*teams)[i].Policy.StorageGiB = 1
			(*teams)[i].Policy.ModelStorageGiB = 1
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	created := departmentRequest(a, "POST", "/teams", "admin", "", `{"name":"Default pools"}`)
	if created.Code != 201 {
		t.Fatalf("%d %s", created.Code, created.Body.String())
	}
	var team Team
	if err := json.Unmarshal(created.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	if team.Policy.StorageGiB != 200 || team.Policy.ModelStorageGiB != 500 {
		t.Fatalf("unexpected defaults: %+v", team.Policy)
	}
	for _, body := range []string{`{"name":"Invalid model pool","model_storage_gib":0}`, `{"name":"Invalid model pool","model_storage_gib":-1}`} {
		rejected := departmentRequest(a, "POST", "/teams", "admin", "", body)
		if rejected.Code != 400 {
			t.Fatalf("invalid declared pool accepted: %d %s", rejected.Code, rejected.Body.String())
		}
	}
	legacy := TeamPolicy{StorageGiB: 3}
	normalizeStoragePolicy(&legacy)
	if legacy.StorageGiB != 3 || legacy.ModelStorageGiB != 3 {
		t.Fatal("legacy allocation silently expanded")
	}
	p := defaultTeamPolicy()
	p.ModelStorageGiB = -1
	if err := validateTeam(&Team{Name: "Invalid", Policy: p}); err == nil {
		t.Fatal("negative model pool accepted")
	}
}

func TestWorkspacesAndClusterJobListing(t *testing.T) {
	a, _, teams := departmentTestApp(t)
	alice := teams[0]

	// 1. Non-admin cannot create a workspace
	r := departmentRequest(a, "POST", "/workspaces", "alice", "", `{"name":"Personal Workspace","type":"personal"}`)
	if r.Code != 403 {
		t.Fatalf("expected 403 for non-admin workspace creation, got %d", r.Code)
	}

	// Shrink existing teams to make budget for new workspaces
	if err := a.teams.mutate(context.Background(), func(list *[]Team) error {
		for i := range *list {
			(*list)[i].Policy.StorageGiB = 10
			(*list)[i].Policy.ModelStorageGiB = 10
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Admin creates a personal workspace for alice with type "personal"
	r = departmentRequest(a, "POST", "/workspaces", "admin", "", `{"name":"Alice Personal","type":"personal","description":"Alice quota","storage_gib":20,"model_storage_gib":20,"user_id":"alice"}`)
	if r.Code != 201 {
		t.Fatalf("admin personal workspace creation failed: %d %s", r.Code, r.Body.String())
	}
	var createdWs Workspace
	if err := json.Unmarshal(r.Body.Bytes(), &createdWs); err != nil {
		t.Fatal(err)
	}
	if createdWs.Type != WorkspaceTypePersonal {
		t.Fatalf("expected type personal, got %s", createdWs.Type)
	}
	if createdWs.Description != "Alice quota" {
		t.Fatalf("expected description 'Alice quota', got %s", createdWs.Description)
	}
	if len(createdWs.Members) != 1 || createdWs.Members[0].ID != "alice" {
		t.Fatalf("expected alice enrolled in personal workspace, got %+v", createdWs.Members)
	}

	// 3. User lists workspaces via /workspaces
	r = departmentRequest(a, "GET", "/workspaces", "alice", "", "")
	if r.Code != 200 {
		t.Fatalf("list workspaces failed: %d %s", r.Code, r.Body.String())
	}
	var wsList struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &wsList); err != nil {
		t.Fatal(err)
	}
	// Alice should see team A and her personal workspace
	hasPersonal := false
	for _, ws := range wsList.Workspaces {
		if ws.ID == createdWs.ID {
			hasPersonal = true
		}
	}
	if !hasPersonal {
		t.Fatal("alice did not see her personal workspace")
	}

	// 4. Submit a job in alice's team
	r = departmentRequest(a, "POST", "/jobs", "alice", alice.ID, `{"command":["echo","hello"]}`)
	if r.Code != 201 {
		t.Fatalf("job submission failed: %d %s", r.Code, r.Body.String())
	}
	var jobResp struct {
		JobID string              `json:"job_id"`
		Job   KubernetesJobStatus `json:"job"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &jobResp)

	// 5. Query cluster jobs via /jobs?cluster=true
	r = departmentRequest(a, "GET", "/jobs?cluster=true", "alice", "", "")
	if r.Code != 200 {
		t.Fatalf("get cluster jobs failed: %d %s", r.Code, r.Body.String())
	}
	var clusterJobs struct {
		Jobs []KubernetesJobStatus `json:"jobs"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &clusterJobs)
	if len(clusterJobs.Jobs) == 0 {
		t.Fatal("cluster jobs list was empty")
	}

	// 6. Test cancel protection: Enroll Bob in Alice's team, but Bob is not the creator or admin
	r = departmentRequest(a, "POST", "/workspaces/"+alice.ID+"/members", "admin", "", `{"user_id":"bob","common_writer":false}`)
	if r.Code != 200 {
		t.Fatalf("failed to add bob to alice's team: %d %s", r.Code, r.Body.String())
	}
	r = departmentRequest(a, "POST", "/jobs/"+jobResp.JobID+"/cancel", "bob", alice.ID, "")
	if r.Code != 403 {
		t.Fatalf("expected 403 when non-owner/non-admin bob cancels job, got %d %s", r.Code, r.Body.String())
	}

	// 7. Alice can cancel her own job
	r = departmentRequest(a, "POST", "/jobs/"+jobResp.JobID+"/cancel", "alice", alice.ID, "")
	if r.Code != 200 {
		t.Fatalf("expected 200 when owner alice cancels job, got %d %s", r.Code, r.Body.String())
	}
}

func TestTeamVolumesUseEthernetFolderExportsAndRejectStaleBindings(t *testing.T) {
	a, client, teams := departmentTestApp(t)
	t.Setenv("MIST_NFS_SERVER", "192.168.240.2")
	t.Setenv("MIST_NFS_ROOT", "/srv/mist-data")
	team := teams[0]
	ctx := context.Background()
	if err := a.teams.provisionNamespace(ctx, &team); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"team-datasets", "datasets"}, {"team-results", "results"}} {
		pv, err := client.CoreV1().PersistentVolumes().Get(ctx, team.namespace()+"-"+pair[0], metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if pv.Spec.NFS.Server != "192.168.240.2" || pv.Spec.NFS.Path != "/srv/mist-data/teams/"+team.ID+"/"+pair[1] {
			t.Fatalf("wrong Ethernet export: %+v", pv.Spec.NFS)
		}
		pv.Spec.NFS.Path = "/obsolete-export"
		if _, err = client.CoreV1().PersistentVolumes().Update(ctx, pv, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		err = a.teams.ensureVolume(ctx, team.ID, team.namespace(), pair[0], "teams/"+team.ID+"/"+pair[1], 1, false)
		if err == nil {
			t.Fatal("stale export binding accepted")
		}
	}
}
