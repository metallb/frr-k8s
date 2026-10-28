// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/google/go-cmp/cmp"
	"github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/controller"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateMixedPasswordConfigurations(t *testing.T) {
	makeConfig := func(name string, neighbor v1beta1.Neighbor) v1beta1.FRRConfiguration {
		return v1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TestNamespace},
			Spec: v1beta1.FRRConfigurationSpec{
				BGP: v1beta1.BGPConfig{Routers: []v1beta1.Router{{
					ASN:       65001,
					Neighbors: []v1beta1.Neighbor{neighbor},
				}}},
			},
		}
	}
	secret := makeConfig("secret", v1beta1.Neighbor{
		ASN: 65002, Address: "192.168.111.1",
		PasswordSecret: v1beta1.SecretReference{Name: "my-secret", Namespace: TestNamespace},
	})
	inline := makeConfig("inline", v1beta1.Neighbor{
		ASN: 65002, Address: "192.168.111.1", Password: "password",
	})
	unrelated := makeConfig("unrelated", v1beta1.Neighbor{ASN: 65002, Address: "192.168.111.2"})
	conflicting := makeConfig("conflicting", v1beta1.Neighbor{
		ASN: 65002, Address: "192.168.111.1", Password: "other-password",
	})

	originalNodes, originalConfigs, originalValidate, originalLogger := getNodes, getFRRConfigurations, Validate, Logger
	t.Cleanup(func() {
		getNodes, getFRRConfigurations, Validate, Logger = originalNodes, originalConfigs, originalValidate, originalLogger
	})
	Logger = log.NewNopLogger()
	Validate = controller.Validate
	getNodes = func() ([]corev1.Node, error) {
		return []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "testnode1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "testnode2"}},
		}, nil
	}

	tests := []struct {
		name     string
		existing []v1beta1.FRRConfiguration
		config   v1beta1.FRRConfiguration
		update   bool
		wantErr  string
	}{
		{
			name: "create inline after secret", existing: []v1beta1.FRRConfiguration{secret}, config: inline,
		},
		{
			name: "create secret after inline", existing: []v1beta1.FRRConfiguration{inline}, config: secret,
		},
		{
			name: "update inline while both exist", existing: []v1beta1.FRRConfiguration{secret, inline}, config: inline, update: true,
		},
		{
			name: "unrelated update while both exist", existing: []v1beta1.FRRConfiguration{secret, inline, unrelated}, config: unrelated, update: true,
		},
		{
			name: "known conflict is still rejected", existing: []v1beta1.FRRConfiguration{secret, inline}, config: conflicting,
			wantErr: "multiple passwords specified for 192.168.111.1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := (&v1beta1.FRRConfigurationList{Items: test.existing}).DeepCopy()
			beforeExisting := existing.DeepCopy()
			config := test.config.DeepCopy()
			beforeConfig := config.DeepCopy()
			getFRRConfigurations = func() (*v1beta1.FRRConfigurationList, error) { return existing, nil }
			var err error
			if test.update {
				_, err = validateConfigUpdate(config)
			} else {
				_, err = validateConfigCreate(config)
			}
			if test.wantErr == "" && err != nil {
				t.Errorf("unexpected admission error: %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Errorf("expected error containing %q, got %v", test.wantErr, err)
			}
			if diff := cmp.Diff(beforeExisting, existing); diff != "" {
				t.Errorf("admission changed existing configurations (-before +after):\n%s", diff)
			}
			if diff := cmp.Diff(beforeConfig, config); diff != "" {
				t.Errorf("admission changed requested configuration (-before +after):\n%s", diff)
			}
		})
	}
}
