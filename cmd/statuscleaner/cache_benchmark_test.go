// SPDX-License-Identifier:Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
)

// Run without a cluster: go test ./cmd/statuscleaner -run '^$' -bench BenchmarkCleaner -benchmem.
// These synthetic workloads isolate object retention and copying, not manager RSS
// or end-to-end performance. Both variants remove managed fields.
//
// BenchmarkCleanerCacheUpdates replaces objects in a fixed-size slice representing
// cached objects. B/op includes full JSON decoding in both variants. The separate
// retained-B/object metric estimates post-GC heap above an empty-slice baseline;
// it excludes cache indexes and can include small runtime/decoder overheads.
func BenchmarkCleanerCacheUpdates(b *testing.B) {
	for _, tc := range []struct {
		name      string
		fixture   any
		newObject func() any
		strip     func(any) (any, error)
	}{
		{
			name: "Pod", fixture: cleanerBenchmarkPod(),
			newObject: func() any { return &corev1.Pod{} }, strip: stripFRRPodForCleaner,
		},
		{
			name: "FRRNodeState",
			fixture: &frrk8sv1beta1.FRRNodeState{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
				Status: frrk8sv1beta1.FRRNodeStateStatus{
					RunningConfig:    strings.Repeat("router bgp 64512\n", 1024), // 16 KiB.
					LastReloadResult: "success", LastConversionResult: "success",
				},
			},
			newObject: func() any { return &frrk8sv1beta1.FRRNodeState{} }, strip: stripFRRNodeStateStatus,
		},
	} {
		for _, stripped := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/strip=%t", tc.name, stripped), func(b *testing.B) {
				wire, err := json.Marshal(tc.fixture)
				if err != nil {
					b.Fatal(err)
				}
				transform := cache.TransformStripManagedFields()
				if stripped {
					transform = tc.strip
				}
				decode := func() any {
					obj := tc.newObject()
					if err := json.Unmarshal(wire, obj); err != nil {
						b.Fatal(err)
					}
					obj, err := transform(obj)
					if err != nil {
						b.Fatal(err)
					}
					return obj
				}
				// Decode fresh objects: DeepCopy shares string contents and would
				// conceal the retention benefit of dropping configuration strings.
				objects := make([]any, 128)
				_ = decode() // Warm decoder metadata before measuring the baseline.
				// Two GCs also clear temporary buffers held in sync.Pool.
				runtime.GC()
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				for i := range objects {
					objects[i] = decode()
				}
				b.ReportAllocs()
				b.ResetTimer()
				next := 0
				for b.Loop() {
					objects[next] = decode()
					next = (next + 1) % len(objects)
				}
				b.StopTimer()
				runtime.GC()
				runtime.GC()
				runtime.ReadMemStats(&after)
				b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc))/float64(len(objects)), "retained-B/object")
				runtime.KeepAlive(objects)
				runtime.KeepAlive(wire)
				runtime.KeepAlive(tc)
			})
		}
	}
}

var cleanerBenchmarkPodCopy *corev1.Pod

// BenchmarkCleanerPodDeepCopy models copying a Pod returned by a cached read.
func BenchmarkCleanerPodDeepCopy(b *testing.B) {
	for _, stripped := range []bool{false, true} {
		b.Run(fmt.Sprintf("strip=%t", stripped), func(b *testing.B) {
			pod := cleanerBenchmarkPod()
			transform := cache.TransformStripManagedFields()
			if stripped {
				transform = stripFRRPodForCleaner
			}
			if _, err := transform(pod); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				cleanerBenchmarkPodCopy = pod.DeepCopy()
			}
			b.StopTimer()
			cleanerBenchmarkPodCopy = nil
		})
	}
}

func cleanerBenchmarkPod() *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "frr-node-1", Namespace: "frr-k8s-system",
			Labels:        map[string]string{"app.kubernetes.io/component": "frr-k8s"},
			ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "benchmark"}},
		},
		Spec:   corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	for i := 0; i < 3; i++ {
		container := corev1.Container{Name: fmt.Sprintf("container-%d", i), Image: "example.invalid/frr:test"}
		for j := 0; j < 16; j++ {
			container.Env = append(container.Env, corev1.EnvVar{Name: fmt.Sprintf("ENV_%d", j), Value: strings.Repeat("x", 256)})
		}
		pod.Spec.Containers = append(pod.Spec.Containers, container)
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: container.Name, Ready: true})
	}
	return pod
}
