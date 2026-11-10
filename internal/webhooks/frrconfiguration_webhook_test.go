// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/google/go-cmp/cmp"
	"github.com/metallb/frr-k8s/api/v1beta1"
	v1core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const TestNamespace = "test-namespace"

var existingConfig = v1beta1.FRRConfiguration{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-config",
		Namespace: TestNamespace,
	},
}

func TestValidateFRRConfiguration(t *testing.T) {
	Logger = log.NewNopLogger()
	toRestore := getFRRConfigurations
	toRestoreNodes := getNodes
	getNodes = func() ([]v1core.Node, error) {
		return []v1core.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testnode",
					Labels: map[string]string{
						"mode": "test",
					},
				},
			},
		}, nil
	}

	defer func() {
		getFRRConfigurations = toRestore
		getNodes = toRestoreNodes
	}()

	tests := []struct {
		desc         string
		before       *v1beta1.FRRConfiguration
		config       *v1beta1.FRRConfiguration
		isNew        bool
		failValidate bool
		expected     *v1beta1.FRRConfigurationList
		warnings     []string
	}{
		{
			desc:   "Second config",
			before: &existingConfig,
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
			},
			isNew: true,
			expected: &v1beta1.FRRConfigurationList{
				Items: []v1beta1.FRRConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-config",
							Namespace: TestNamespace,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: TestNamespace,
						},
					},
				},
			},
		},
		{
			desc:   "Same, update",
			before: &existingConfig,
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
			},
			isNew: false,
			expected: &v1beta1.FRRConfigurationList{
				Items: []v1beta1.FRRConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-config",
							Namespace: TestNamespace,
						},
					},
				},
			},
		},
		{
			desc:   "Same, new",
			before: &existingConfig,
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
			},
			isNew: true,
			expected: &v1beta1.FRRConfigurationList{
				Items: []v1beta1.FRRConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-config",
							Namespace: TestNamespace,
						},
					},
				},
			},
			failValidate: true,
		},
		{
			desc: "Validation should fail if created with an invalid nodeSelector",
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config1",
					Namespace: TestNamespace,
				},
				Spec: v1beta1.FRRConfigurationSpec{
					NodeSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "@",
						},
					},
				},
			},
			isNew:        true,
			expected:     nil,
			failValidate: true,
		},
		{
			desc: "return warning when enableGracefulRestart changes",
			before: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:                   65002,
										EnableGracefulRestart: false,
									},
								},
							},
						},
					},
				},
			},
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:                   65002,
										EnableGracefulRestart: true,
									},
								},
							},
						},
					},
				},
			},
			isNew: false,
			expected: &v1beta1.FRRConfigurationList{
				Items: []v1beta1.FRRConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-config",
							Namespace: TestNamespace,
						},
						Spec: v1beta1.FRRConfigurationSpec{
							BGP: v1beta1.BGPConfig{
								Routers: []v1beta1.Router{
									{
										Neighbors: []v1beta1.Neighbor{
											{
												ASN:                   65002,
												EnableGracefulRestart: true,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			failValidate: false,
			warnings:     []string{"Graceful restart configuration changed, it will be available on the next restart"},
		},
		{
			desc:   "warning when disableMP is set",
			before: &existingConfig,
			config: &v1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
				Spec: v1beta1.FRRConfigurationSpec{
					BGP: v1beta1.BGPConfig{
						Routers: []v1beta1.Router{
							{
								Neighbors: []v1beta1.Neighbor{
									{
										ASN:       65002,
										DisableMP: true,
									},
								},
							},
						},
					},
				},
			},
			isNew: true,
			expected: &v1beta1.FRRConfigurationList{
				Items: []v1beta1.FRRConfiguration{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-config",
							Namespace: TestNamespace,
						},
						Spec: v1beta1.FRRConfigurationSpec{
							BGP: v1beta1.BGPConfig{
								Routers: []v1beta1.Router{
									{
										Neighbors: []v1beta1.Neighbor{
											{
												ASN:       65002,
												DisableMP: true,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			failValidate: false,
			warnings:     []string{"disableMP is deprecated, is ignored and will be removed in a future release"},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			var err error
			getFRRConfigurations = func() (*v1beta1.FRRConfigurationList, error) {
				return &v1beta1.FRRConfigurationList{
					Items: []v1beta1.FRRConfiguration{
						*test.before,
					},
				}, nil
			}
			mock := &mockValidator{}
			Validate = mock.Validate
			mock.forceError = test.failValidate

			var warnings []string

			if test.isNew {
				warnings, err = validateConfigCreate(test.config)
			} else {
				warnings, err = validateConfigUpdate(test.config)
			}
			if test.failValidate && err == nil {
				t.Fatalf("test %s failed, expecting error", test.desc)
			}

			if !cmp.Equal(test.warnings, warnings) {
				t.Fatalf("admission warning message don't match: expected '%#v', got '%#v'", test.warnings, warnings)
			}

			if !cmp.Equal(test.expected, mock.configs) {
				t.Fatalf("test %s failed, %s", test.desc, cmp.Diff(test.expected, mock.configs))
			}
		})
	}
}

func TestConfigsPerMatchingNode(t *testing.T) {
	nodes := []v1core.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-0",
				Labels: map[string]string{"kubernetes.io/hostname": "node-0", "role": "worker"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-1",
				Labels: map[string]string{"kubernetes.io/hostname": "node-1", "role": "worker"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "node-2",
				Labels: map[string]string{"kubernetes.io/hostname": "node-2", "role": "infra"},
			},
		},
	}

	config := func(name string, nodeSelector metav1.LabelSelector) v1beta1.FRRConfiguration {
		return v1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TestNamespace},
			Spec:       v1beta1.FRRConfigurationSpec{NodeSelector: nodeSelector},
		}
	}
	incoming := config("incoming", metav1.LabelSelector{})
	everything := config("everything", metav1.LabelSelector{})
	workers := config("workers", metav1.LabelSelector{MatchLabels: map[string]string{"role": "worker"}})
	node0 := config("node-0", metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "node-0"}})
	unmatched := config("unmatched", metav1.LabelSelector{MatchLabels: map[string]string{"role": "control-plane"}})
	invalid := config("invalid", metav1.LabelSelector{MatchLabels: map[string]string{"app": "@"}})

	oldCfg := config("incoming", metav1.LabelSelector{})
	oldCfg.Spec.BGP.Routers = []v1beta1.Router{
		{
			Neighbors: []v1beta1.Neighbor{
				{ASN: 64512, Address: "10.0.4.63"},
			},
		},
	}
	updated := oldCfg.DeepCopy()
	updated.Spec.BGP.Routers[0].Neighbors[0].EnableGracefulRestart = true
	workerConfig := incoming.DeepCopy()
	workerConfig.Spec.NodeSelector = workers.Spec.NodeSelector
	unmatchedUpdate := updated.DeepCopy()
	unmatchedUpdate.Spec.NodeSelector = unmatched.Spec.NodeSelector

	tests := []struct {
		desc     string
		config   v1beta1.FRRConfiguration
		existing []v1beta1.FRRConfiguration
		expected map[string][]v1beta1.FRRConfiguration
		warnings []string
	}{
		{
			desc:     "fans every config out to all the nodes it selects",
			config:   incoming,
			existing: []v1beta1.FRRConfiguration{everything, workers, node0, unmatched},
			expected: map[string][]v1beta1.FRRConfiguration{
				"node-0": {everything, workers, node0, incoming},
				"node-1": {everything, workers, incoming},
				"node-2": {everything, incoming},
			},
		},
		{
			desc:     "replaces the existing config of the same name for every node",
			config:   *updated,
			existing: []v1beta1.FRRConfiguration{oldCfg},
			expected: map[string][]v1beta1.FRRConfiguration{
				"node-0": {*updated},
				"node-1": {*updated},
				"node-2": {*updated},
			},
			warnings: []string{"Graceful restart configuration changed, it will be available on the next restart"},
		},
		{
			desc:     "only groups nodes selected by the incoming config",
			config:   *workerConfig,
			existing: []v1beta1.FRRConfiguration{everything, workers, node0, unmatched},
			expected: map[string][]v1beta1.FRRConfiguration{
				"node-0": {everything, workers, node0, *workerConfig},
				"node-1": {everything, workers, *workerConfig},
			},
		},
		{
			desc:     "returns no groups or restart warnings when no nodes match",
			config:   *unmatchedUpdate,
			existing: []v1beta1.FRRConfiguration{everything, oldCfg},
			expected: map[string][]v1beta1.FRRConfiguration{},
		},
		{
			desc:     "skips existing configs with invalid selectors",
			config:   incoming,
			existing: []v1beta1.FRRConfiguration{invalid, everything},
			expected: map[string][]v1beta1.FRRConfiguration{
				"node-0": {everything, incoming},
				"node-1": {everything, incoming},
				"node-2": {everything, incoming},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			selector, err := metav1.LabelSelectorAsSelector(&test.config.Spec.NodeSelector)
			if err != nil {
				t.Fatalf("invalid node selector: %v", err)
			}
			grouped, warnings := configsPerMatchingNode(&test.config, selector, nodes, &v1beta1.FRRConfigurationList{Items: test.existing})
			configs := make(map[string][]v1beta1.FRRConfiguration, len(grouped))
			for _, n := range grouped {
				configs[n.name] = n.cfgs.Items
			}
			if diff := cmp.Diff(test.expected, configs); diff != "" {
				t.Fatalf("unexpected configs per node (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.warnings, warnings); diff != "" {
				t.Fatalf("unexpected warnings (-want +got):\n%s", diff)
			}
		})
	}
}
