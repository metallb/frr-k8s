// SPDX-License-Identifier:Apache-2.0

package v1beta1

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/google/go-cmp/cmp"
	v1core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const TestNamespace = "test-namespace"

func TestValidateFRRConfiguration(t *testing.T) {
	Logger = log.NewNopLogger()
	existingConfig := FRRConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: TestNamespace,
		},
	}

	toRestore := getFRRConfigurations
	getFRRConfigurations = func() (*FRRConfigurationList, error) {
		return &FRRConfigurationList{
			Items: []FRRConfiguration{
				existingConfig,
			},
		}, nil
	}
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
		config       *FRRConfiguration
		isNew        bool
		failValidate bool
		expected     *FRRConfigurationList
	}{
		{
			desc: "Second config",
			config: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
			},
			isNew: true,
			expected: &FRRConfigurationList{
				Items: []FRRConfiguration{
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
			desc: "Same, update",
			config: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
			},
			isNew: false,
			expected: &FRRConfigurationList{
				Items: []FRRConfiguration{
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
			desc: "Same, new",
			config: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: TestNamespace,
				},
			},
			isNew: true,
			expected: &FRRConfigurationList{
				Items: []FRRConfiguration{
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
			config: &FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config1",
					Namespace: TestNamespace,
				},
				Spec: FRRConfigurationSpec{
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
	}
	for _, test := range tests {
		var err error
		mock := &mockValidator{}
		Validate = mock.Validate
		mock.forceError = test.failValidate

		if test.isNew {
			_, err = test.config.ValidateCreate()
		} else {
			_, err = test.config.ValidateUpdate(nil)
		}
		if test.failValidate && err == nil {
			t.Fatalf("test %s failed, expecting error", test.desc)
		}
		if !cmp.Equal(test.expected, mock.configs) {
			t.Fatalf("test %s failed, %s", test.desc, cmp.Diff(test.expected, mock.configs))
		}
	}
}
