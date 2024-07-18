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
