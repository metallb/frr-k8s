// SPDX-License-Identifier:Apache-2.0

package main

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
)

func TestStripFRRPodForCleaner(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "frr-node1", Namespace: "frr-k8s-system", ResourceVersion: "42",
			Labels:        map[string]string{"component": "frr-k8s"},
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "test"}},
		},
		Spec:   corev1.PodSpec{NodeName: "node1", Containers: []corev1.Container{{Name: "large-container-spec"}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	transformed, err := stripFRRPodForCleaner(pod)
	if err != nil {
		t.Fatalf("stripFRRPodForCleaner() returned error: %v", err)
	}
	got, ok := transformed.(*corev1.Pod)
	if !ok {
		t.Fatalf("stripFRRPodForCleaner() returned %T", transformed)
	}
	if got.Name != "frr-node1" || got.Namespace != "frr-k8s-system" || got.ResourceVersion != "42" || got.Labels["component"] != "frr-k8s" {
		t.Fatalf("object metadata was not preserved: %#v", got.ObjectMeta)
	}
	if got.Spec.NodeName != "node1" || len(got.Spec.Containers) != 0 {
		t.Fatalf("only spec.nodeName should remain: %#v", got.Spec)
	}
	if !reflect.DeepEqual(got.Status, corev1.PodStatus{}) {
		t.Fatalf("status was not removed: %#v", got.Status)
	}
	if got.ManagedFields != nil {
		t.Fatalf("managed fields were not removed: %#v", got.ManagedFields)
	}
}

func TestStripFRRNodeStateStatus(t *testing.T) {
	nodeState := &frrk8sv1beta1.FRRNodeState{
		ObjectMeta: metav1.ObjectMeta{Name: "node1", ResourceVersion: "42", Labels: map[string]string{"test": "value"}, ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "test"}}},
		Status:     frrk8sv1beta1.FRRNodeStateStatus{RunningConfig: "large running configuration", LastConversionResult: "success", LastReloadResult: "success"},
	}
	transformed, err := stripFRRNodeStateStatus(nodeState)
	if err != nil {
		t.Fatalf("stripFRRNodeStateStatus() returned error: %v", err)
	}
	got, ok := transformed.(*frrk8sv1beta1.FRRNodeState)
	if !ok {
		t.Fatalf("stripFRRNodeStateStatus() returned %T", transformed)
	}
	if got.Name != "node1" || got.ResourceVersion != "42" || got.Labels["test"] != "value" {
		t.Fatalf("object metadata was not preserved: %#v", got.ObjectMeta)
	}
	if got.Status != (frrk8sv1beta1.FRRNodeStateStatus{}) {
		t.Fatalf("status was not removed: %#v", got.Status)
	}
	if got.ManagedFields != nil {
		t.Fatalf("managed fields were not removed: %#v", got.ManagedFields)
	}
}

func TestStripFRRNodeStateStatusIgnoresOtherTypes(t *testing.T) {
	obj := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
	transformed, err := stripFRRNodeStateStatus(obj)
	if err != nil {
		t.Fatalf("stripFRRNodeStateStatus() returned error: %v", err)
	}
	if transformed != obj {
		t.Fatalf("stripFRRNodeStateStatus() replaced an unrelated object")
	}
}
