// SPDX-License-Identifier:Apache-2.0

package v1beta2

import (
	"fmt"
	"testing"

	apiv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	"github.com/metallb/frr-k8s/internal/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func BenchmarkValidateConfig(b *testing.B) {
	nodes := generateNodes(100)
	configs := generateFRRConfigurations(nodes, 20)
	originalGetNodes := getNodes
	originalGetFRRConfigurations := getFRRConfigurations
	originalValidate := Validate

	defer func() {
		getNodes = originalGetNodes
		getFRRConfigurations = originalGetFRRConfigurations
		Validate = originalValidate
	}()

	getNodes = func() ([]corev1.Node, error) {
		return nodes, nil
	}

	getFRRConfigurations = func() (*apiv1beta2.FRRConfigurationList, error) {
		return &apiv1beta2.FRRConfigurationList{Items: configs}, nil
	}

	Validate = controller.Validate

	testConfig := &apiv1beta2.FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "default",
		},
		Spec: apiv1beta2.FRRConfigurationSpec{
			NodeSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"node-id": "node-0",
				},
			},
			BGP: apiv1beta2.BGPConfig{
				Routers: []apiv1beta2.Router{
					{
						ASN: 65001,
						ID:  "192.168.1.1",
						VRF: "",
						Neighbors: []apiv1beta2.Neighbor{
							{
								ASN:     65002,
								Address: "192.168.1.2",
								Port:    ptr.To[uint16](179),
							},
						},
						Prefixes: []string{"192.168.1.0/24"},
					},
				},
			},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := validateConfig(testConfig)
		if err != nil {
			b.Fatalf("validation failed: %v", err)
		}
	}
}

func generateNodes(count int) []corev1.Node {
	nodes := make([]corev1.Node, count)
	for i := range count {
		nodes[i] = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("node-%d", i),
				Labels: map[string]string{
					"node-id": fmt.Sprintf("node-%d", i),
				},
			},
		}
	}
	return nodes
}

func generateFRRConfigurations(nodes []corev1.Node, configsPerNode int) []apiv1beta2.FRRConfiguration {
	totalConfigs := len(nodes) * configsPerNode
	configs := make([]apiv1beta2.FRRConfiguration, totalConfigs)

	configIndex := 0
	for _, node := range nodes {
		for range configsPerNode {
			configs[configIndex] = apiv1beta2.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("frr-config-%d", configIndex),
					Namespace: "default",
				},
				Spec: apiv1beta2.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: node.Labels,
					},
					BGP: apiv1beta2.BGPConfig{
						Routers: []apiv1beta2.Router{
							{
								ASN: 65001,
								ID:  "192.168.1.1",
								VRF: "",
								Neighbors: []apiv1beta2.Neighbor{
									{
										ASN:     65002,
										Address: "192.168.1.2",
										Port:    ptr.To[uint16](179),
									},
								},
								Prefixes: []string{"192.168.1.0/24"},
							},
						},
					},
				},
			}
			configIndex++
		}
	}
	return configs
}
