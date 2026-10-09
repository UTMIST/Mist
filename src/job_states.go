package main

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A list response reads each collection once. History polling must not issue
// one pod/event/claim request per retained job (the pilot retains all jobs).
type jobStateData struct {
	pods   map[string][]corev1.Pod
	events []corev1.Event
	claims map[string]resourcev1.ResourceClaim
}

func (e *KubernetesExecutor) loadJobState(ctx context.Context) (*jobStateData, error) {
	pods, err := e.client.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{LabelSelector: managedLabel + "=mist,mist.io/owner=" + e.owner})
	if err != nil {
		return nil, err
	}
	data := &jobStateData{pods: map[string][]corev1.Pod{}, claims: map[string]resourcev1.ResourceClaim{}}
	needEvents, needClaims := false, false
	for _, pod := range pods.Items {
		name := pod.Labels["batch.kubernetes.io/job-name"]
		data.pods[name] = append(data.pods[name], pod)
		needEvents = needEvents || (pod.Status.Phase == corev1.PodPending && pod.Spec.NodeName == "")
		needClaims = needClaims || len(pod.Status.ResourceClaimStatuses) > 0
	}
	if needEvents {
		events, err := e.client.CoreV1().Events(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		data.events = events.Items
	}
	if needClaims {
		claims, err := e.client.ResourceV1().ResourceClaims(e.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, claim := range claims.Items {
			data.claims[claim.Name] = claim
		}
	}
	return data, nil
}
