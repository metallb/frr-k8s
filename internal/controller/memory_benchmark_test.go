// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"testing"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BenchmarkConfigsForNodeThreeThousandNodes(b *testing.B) {
	configs := make([]frrk8sv1beta1.FRRConfiguration, 0, 6000)
	for node := 0; node < 3000; node++ {
		hostname := fmt.Sprintf("sim-host-%d", node)
		for advertisement := 0; advertisement < 2; advertisement++ {
			configs = append(configs, frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("config-%d-%d", node, advertisement)},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{NodeSelector: metav1.LabelSelector{MatchLabels: map[string]string{
					corev1.LabelHostname: hostname,
				}}},
			})
		}
	}

	nodeLabels := map[string]string{corev1.LabelHostname: "sim-host-1500"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		matching, err := configsForNode(configs, nodeLabels)
		if err != nil {
			b.Fatal(err)
		}
		if len(matching) != 2 {
			b.Fatalf("expected 2 matching configurations, got %d", len(matching))
		}
	}
}
