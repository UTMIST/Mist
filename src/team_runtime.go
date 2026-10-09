package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var registryPattern = regexp.MustCompile(`^[a-z0-9]+([.-][a-z0-9]+)*\.[a-z]{2,}$`)

type permissionError struct{ error }

func memberOwner(id string) string  { m := &Member{ID: id}; return accountOwner(m) }
func accountOwner(m *Member) string { return "member-" + hashID(m.ID) }
func validateImageReference(image string, registries []string, local map[string]bool) error {
	if local[image] && strings.HasPrefix(image, "mist-training:") {
		return nil
	}
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return errors.New("invalid container image reference")
	}
	if !contains(registries, reference.Domain(named)) {
		return errors.New("image registry is not allowed by this team's policy")
	}
	if _, tagged := named.(reference.Tagged); !tagged {
		if _, digested := named.(reference.Digested); !digested {
			return errors.New("include an image tag or sha256 digest")
		}
	}
	return nil
}
func validateTeamRequest(t *Team, m *Member, req *CreateJobRequest) error {
	if t.Disabled || t.member(m.ID) == nil {
		return &permissionError{errors.New("team membership was revoked")}
	}
	p := t.Policy
	if quantity(req.CPU).Cmp(resource.MustParse(p.CPU)) > 0 || quantity(req.Memory).Cmp(resource.MustParse(p.Memory)) > 0 {
		return errors.New("job exceeds the team's CPU or memory limit")
	}
	if req.Accelerator == "nvidia" && req.DeviceCount > p.NVIDIA || req.Accelerator == "tenstorrent" && req.DeviceCount > p.Tenstorrent {
		return errors.New("job exceeds the team's accelerator limit")
	}
	if req.TimeoutSeconds > p.RuntimeSeconds {
		return errors.New("job runtime exceeds team policy")
	}
	if req.StorageScope == "" {
		req.StorageScope = m.ID
	}
	if req.StorageScope != m.ID && (req.StorageScope != "common" || (!t.member(m.ID).CommonWriter && !isAdmin(m))) {
		return &permissionError{errors.New("You can write your own folder; common-folder writes require permission")}
	}
	if req.TTRuntime != "" && req.TTRuntime != "host" && req.TTRuntime != "container" {
		return errors.New("tt_runtime must be host or container")
	}
	if req.Accelerator != "tenstorrent" && req.TTRuntime != "" {
		return errors.New("tt_runtime applies only to Tenstorrent jobs")
	}
	if req.Accelerator == "tenstorrent" {
		if req.TTRuntime == "" {
			if req.Image == ttImage {
				req.TTRuntime = "host"
			} else {
				req.TTRuntime = "container"
			}
		}
		if req.TTRuntime == "host" && req.Image != ttImage {
			return errors.New("host TT runtime requires the tested default image; use container runtime for custom images")
		}
		if req.Type == "training-smoke" && (req.Image != ttImage || req.TTRuntime != "host") {
			return errors.New("TT training check requires the tested host runtime")
		}
	}
	return nil
}
func atomicJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mist-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (s *TeamService) provisionRequest(t *Team) error {
	return atomicJSON(filepath.Join(s.app.executor.storage.root, "metadata/provision-requests", t.ID+".json"), map[string]any{"id": t.ID, "storage_gib": t.Policy.StorageGiB, "model_storage_gib": t.Policy.ModelStorageGiB})
}
func (s *TeamService) provisionNamespace(ctx context.Context, t *Team) error {
	signature, _ := json.Marshal(t.Policy)
	cached := s.provisioned[t.ID]
	if cached.signature == string(signature) && time.Since(cached.checked) < 30*time.Second {
		return nil
	}
	c := s.app.executor.client
	ns := t.namespace()
	_, err := c.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = c.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, Labels: map[string]string{"mist.io/workloads": "true", "mist.io/team": t.ID}}}, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}
	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: "team-limits", Namespace: ns}, Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
		"requests.cpu": resource.MustParse(t.Policy.CPU), "limits.cpu": resource.MustParse(t.Policy.CPU), "requests.memory": resource.MustParse(t.Policy.Memory), "limits.memory": resource.MustParse(t.Policy.Memory),
		"requests.nvidia.com/gpu": resource.MustParse(fmt.Sprint(t.Policy.NVIDIA)), "pods": resource.MustParse(fmt.Sprint(t.Policy.Concurrent)),
		"requests.hugepages-1Gi": resource.MustParse(fmt.Sprintf("%dGi", t.Policy.Tenstorrent*2)),
	}}}
	old, err := c.CoreV1().ResourceQuotas(ns).Get(ctx, quota.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = c.CoreV1().ResourceQuotas(ns).Create(ctx, quota, metav1.CreateOptions{})
	} else if err == nil && !quantitiesEqual(old.Spec.Hard, quota.Spec.Hard) {
		quota.ResourceVersion = old.ResourceVersion
		_, err = c.CoreV1().ResourceQuotas(ns).Update(ctx, quota, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	policy := teamNetworkPolicy(ns)
	if _, err = c.NetworkingV1().NetworkPolicies(ns).Get(ctx, policy.Name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		_, err = c.NetworkingV1().NetworkPolicies(ns).Create(ctx, policy, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}
	for _, name := range []string{"training-smoke-script"} {
		if _, err = c.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			source, readErr := c.CoreV1().ConfigMaps(s.app.executor.namespace).Get(ctx, name, metav1.GetOptions{})
			if readErr != nil {
				return readErr
			}
			_, err = c.CoreV1().ConfigMaps(ns).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name}, Data: source.Data}, metav1.CreateOptions{})
		}
		if err != nil {
			return err
		}
	}
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("mist-tenstorrent-%d-boards", i)
		if _, err = c.ResourceV1().ResourceClaimTemplates(ns).Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			source, readErr := c.ResourceV1().ResourceClaimTemplates(s.app.executor.namespace).Get(ctx, name, metav1.GetOptions{})
			if readErr != nil {
				return readErr
			}
			source.ObjectMeta = metav1.ObjectMeta{Name: name}
			_, err = c.ResourceV1().ResourceClaimTemplates(ns).Create(ctx, source, metav1.CreateOptions{})
		}
		if err != nil {
			return err
		}
	}
	if err = s.ensureVolume(ctx, t.ID, ns, "team-storage", fmt.Sprintf("teams/%s", t.ID), t.Policy.StorageGiB, false); err != nil {
		return err
	}
	if err = s.ensureVolume(ctx, t.ID, ns, "team-models", fmt.Sprintf("teams/%s/.models", t.ID), t.Policy.ModelStorageGiB, false); err != nil {
		return err
	}
	s.provisioned[t.ID] = teamProvisioned{signature: string(signature), checked: time.Now()}
	return nil
}
func quantitiesEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || v.Cmp(w) != 0 {
			return false
		}
	}
	return true
}
func teamNetworkPolicy(ns string) *networkv1.NetworkPolicy {
	return &networkv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "team-boundary", Namespace: ns}, Spec: networkv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkv1.PolicyType{networkv1.PolicyTypeIngress, networkv1.PolicyTypeEgress}, Ingress: []networkv1.NetworkPolicyIngressRule{}, Egress: []networkv1.NetworkPolicyEgressRule{
		{To: []networkv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkv1.NetworkPolicyPort{{Protocol: ptr(corev1.ProtocolUDP), Port: ptr(intstr.FromInt32(53))}, {Protocol: ptr(corev1.ProtocolTCP), Port: ptr(intstr.FromInt32(53))}}},
		{To: []networkv1.NetworkPolicyPeer{{IPBlock: &networkv1.IPBlock{CIDR: "0.0.0.0/0", Except: []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}}}}, Ports: []networkv1.NetworkPolicyPort{{Protocol: ptr(corev1.ProtocolTCP), Port: ptr(intstr.FromInt32(443))}}},
	}}}
}
func (s *TeamService) ensureVolume(ctx context.Context, sourceTeam, namespace, claim, path string, gib int64, readonly bool) error {
	c := s.app.executor.client
	name := namespace + "-" + claim
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"mist.io/team": sourceTeam}}, Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", gib))}, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain, StorageClassName: "", ClaimRef: &corev1.ObjectReference{Name: claim, Namespace: namespace}, MountOptions: []string{"nfsvers=4.2", "hard", "timeo=100", "retrans=2"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: envOr("MIST_NFS_SERVER", "10.0.0.112"), Path: "/srv/mist-storage/" + path, ReadOnly: readonly}}}}
	if existing, err := c.CoreV1().PersistentVolumes().Get(ctx, name, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		if _, err = c.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if existing.Spec.Capacity.Storage().Cmp(pv.Spec.Capacity[corev1.ResourceStorage]) < 0 {
		existing.Spec.Capacity = pv.Spec.Capacity
		if _, err = c.CoreV1().PersistentVolumes().Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: claim, Namespace: namespace}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, StorageClassName: ptr(""), VolumeName: name, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", gib))}}}}
	if _, err := c.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claim, metav1.GetOptions{}); apierrors.IsNotFound(err) {
		_, err = c.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
		return err
	} else {
		return err
	}
}
func quantity(value string) *resource.Quantity {
	if value == "" {
		value = "0"
	}
	q := resource.MustParse(value)
	return &q
}
