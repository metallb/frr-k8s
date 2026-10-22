// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidatePasswords(t *testing.T) {
	secret := v1beta1.SecretReference{Name: "bgp-password", Namespace: "frr-k8s-system"}
	tests := []struct {
		name      string
		neighbors []v1beta1.Neighbor
		wantErr   string
	}{
		{
			name:      "missing secret is deferred",
			neighbors: []v1beta1.Neighbor{{PasswordSecret: secret}},
		},
		{
			name: "same secret is deferred",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{PasswordSecret: secret},
			},
		},
		{
			name: "different secrets are deferred",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{PasswordSecret: v1beta1.SecretReference{Name: "other-password"}},
			},
		},
		{
			name: "secret and inline password",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{Password: "password"},
			},
		},
		{
			name: "secret and no password",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{},
			},
		},
		{
			name: "matching inline passwords",
			neighbors: []v1beta1.Neighbor{
				{Password: "password"},
				{Password: "password"},
			},
		},
		{
			name: "conflicting inline passwords",
			neighbors: []v1beta1.Neighbor{
				{Password: "password"},
				{Password: "other-password"},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "inline password and no password conflict",
			neighbors: []v1beta1.Neighbor{
				{Password: "password"},
				{},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "secret does not hide conflicting inline passwords",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{Password: "password"},
				{Password: "other-password"},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "secret between conflicting inline passwords",
			neighbors: []v1beta1.Neighbor{
				{Password: "password"},
				{PasswordSecret: secret},
				{Password: "other-password"},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "secret does not hide inline and no password conflict",
			neighbors: []v1beta1.Neighbor{
				{Password: "password"},
				{PasswordSecret: secret},
				{},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "both password sources are deferred to conversion",
			neighbors: []v1beta1.Neighbor{
				{Password: "password", PasswordSecret: secret},
			},
		},
		{
			name: "both sources retain the known inline password",
			neighbors: []v1beta1.Neighbor{
				{Password: "password", PasswordSecret: secret},
				{PasswordSecret: secret},
			},
		},
		{
			name: "both sources do not hide known password conflicts",
			neighbors: []v1beta1.Neighbor{
				{Password: "password", PasswordSecret: secret},
				{Password: "other-password"},
			},
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name: "mixed sources do not hide ASN conflicts",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{Password: "password", ASN: 65003},
			},
			wantErr: "multiple asns specified for 192.0.2.2",
		},
	}
	for _, test := range tests {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				neighbors := slices.Clone(test.neighbors)
				if reverse {
					slices.Reverse(neighbors)
				}
				configs := passwordTestConfigs(neighbors...)
				before := configs.DeepCopy()
				for attempt := 0; attempt < 2; attempt++ {
					err := Validate(configs)
					if test.wantErr == "" && err != nil {
						t.Errorf("validation %d: unexpected error: %v", attempt, err)
					}
					if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
						t.Errorf("validation %d: expected error containing %q, got %v", attempt, test.wantErr, err)
					}
					if diff := cmp.Diff(before, configs); diff != "" {
						t.Fatalf("validation changed input (-before +after):\n%s", diff)
					}
				}
			})
		}
	}
}

func TestMixedPasswordConversion(t *testing.T) {
	basicSecret := func(password string) *corev1.Secret {
		return &corev1.Secret{
			Type: corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{"password": []byte(password)},
		}
	}
	tests := []struct {
		name           string
		inlinePassword string
		secret         *corev1.Secret
		bothSources    bool
		wantErr        string
		transient      bool
	}{
		{
			name:           "equal resolved passwords",
			inlinePassword: "password",
			secret:         basicSecret("password"),
		},
		{
			name:           "different resolved passwords",
			inlinePassword: "password",
			secret:         basicSecret("other-password"),
			wantErr:        "multiple passwords specified for 192.0.2.2",
		},
		{
			name:           "empty secret conflicts with inline password",
			inlinePassword: "password",
			secret:         basicSecret(""),
			wantErr:        "multiple passwords specified for 192.0.2.2",
		},
		{
			name:   "empty secret matches no authentication",
			secret: basicSecret(""),
		},
		{
			name:    "nonempty secret conflicts with no authentication",
			secret:  basicSecret("password"),
			wantErr: "multiple passwords specified for 192.0.2.2",
		},
		{
			name:           "missing secret",
			inlinePassword: "password",
			wantErr:        "secret bgp-password not found",
			transient:      true,
		},
		{
			name:           "invalid secret type",
			inlinePassword: "password",
			secret:         &corev1.Secret{Type: corev1.SecretTypeOpaque},
			wantErr:        "secret type mismatch",
		},
		{
			name:           "missing password key",
			inlinePassword: "password",
			secret:         &corev1.Secret{Type: corev1.SecretTypeBasicAuth},
			wantErr:        "password field not specified",
		},
		{
			name:           "both sources are rejected during conversion",
			inlinePassword: "password",
			secret:         basicSecret("password"),
			bothSources:    true,
			wantErr:        "specifies both cleartext password and secret ref",
		},
	}
	for _, test := range tests {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				neighbors := []v1beta1.Neighbor{
					{PasswordSecret: v1beta1.SecretReference{Name: "bgp-password", Namespace: "frr-k8s-system"}},
					{Password: test.inlinePassword},
				}
				if test.bothSources {
					neighbors[0].Password = test.inlinePassword
				}
				if reverse {
					slices.Reverse(neighbors)
				}
				configs := passwordTestConfigs(neighbors...)
				if err := Validate(configs); err != nil {
					t.Fatalf("admission should defer secret validation: %v", err)
				}
				secrets := map[string]corev1.Secret{}
				if test.secret != nil {
					secret := *test.secret
					secret.Name = "bgp-password"
					secret.Namespace = "frr-k8s-system"
					secrets[secret.Name] = secret
				}
				config, err := apiToFRR(ClusterResources{
					FRRConfigs:      configs.Items,
					PasswordSecrets: secrets,
				}, nil)
				var transient TransientError
				if got := errors.As(err, &transient); got != test.transient {
					t.Errorf("expected transient=%t, got %v", test.transient, err)
				}
				if test.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), test.wantErr) {
						t.Fatalf("expected error containing %q, got %v", test.wantErr, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected conversion error: %v", err)
				}
				if len(config.Routers) != 1 || len(config.Routers[0].Neighbors) != 1 {
					t.Fatalf("expected one merged neighbor, got %+v", config)
				}
				if got := config.Routers[0].Neighbors[0].Password; got != test.inlinePassword {
					t.Errorf("expected resolved password %q, got %q", test.inlinePassword, got)
				}
			})
		}
	}
}

func TestValidatePasswordNeighborIdentity(t *testing.T) {
	secret := v1beta1.SecretReference{Name: "bgp-password"}
	otherSecret := v1beta1.SecretReference{Name: "other-bgp-password"}
	tests := []struct {
		name      string
		neighbors []v1beta1.Neighbor
		vrfs      []string
	}{
		{
			name: "separate addresses",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{Password: "password"},
				{Address: "192.0.2.3", PasswordSecret: otherSecret},
				{Address: "192.0.2.3", Password: "other-password"},
			},
		},
		{
			name: "same address in separate VRFs",
			neighbors: []v1beta1.Neighbor{
				{PasswordSecret: secret},
				{Password: "password"},
				{PasswordSecret: otherSecret},
				{Password: "other-password"},
			},
			vrfs: []string{"blue", "blue", "red", "red"},
		},
		{
			name: "same interface",
			neighbors: []v1beta1.Neighbor{
				{Interface: "eth0", PasswordSecret: secret},
				{Interface: "eth0", Password: "password"},
			},
		},
		{
			name: "separate interfaces",
			neighbors: []v1beta1.Neighbor{
				{Interface: "eth0", PasswordSecret: secret},
				{Interface: "eth0", Password: "password"},
				{Interface: "eth1", PasswordSecret: otherSecret},
				{Interface: "eth1", Password: "other-password"},
			},
		},
		{
			name: "interface names do not collide with VRF suffixes",
			neighbors: []v1beta1.Neighbor{
				{Interface: "eth0-blue", PasswordSecret: secret},
				{Interface: "eth0-blue", Password: "password"},
				{Interface: "eth0", PasswordSecret: otherSecret},
				{Interface: "eth0", Password: "other-password"},
			},
			vrfs: []string{"", "", "blue", "blue"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configs := passwordTestConfigs(test.neighbors...)
			for i, vrf := range test.vrfs {
				configs.Items[i].Spec.BGP.Routers[0].VRF = vrf
			}
			before := configs.DeepCopy()
			if err := Validate(configs); err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
			if _, err := apiToFRR(ClusterResources{
				FRRConfigs: configs.Items,
				PasswordSecrets: map[string]corev1.Secret{
					"bgp-password": {
						Type: corev1.SecretTypeBasicAuth,
						Data: map[string][]byte{"password": []byte("password")},
					},
					"other-bgp-password": {
						Type: corev1.SecretTypeBasicAuth,
						Data: map[string][]byte{"password": []byte("other-password")},
					},
				},
			}, nil); err != nil {
				t.Errorf("valid identity fixtures must also reconcile: %v", err)
			}
			if diff := cmp.Diff(before, configs); diff != "" {
				t.Fatalf("validation changed input (-before +after):\n%s", diff)
			}
		})
	}
}

func passwordTestConfigs(neighbors ...v1beta1.Neighbor) *v1beta1.FRRConfigurationList {
	configs := &v1beta1.FRRConfigurationList{}
	for i, n := range neighbors {
		if n.ASN == 0 {
			n.ASN = 65002
		}
		if n.Address == "" && n.Interface == "" {
			n.Address = "192.0.2.2"
		}
		configs.Items = append(configs.Items, v1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("config-%d", i)},
			Spec: v1beta1.FRRConfigurationSpec{
				BGP: v1beta1.BGPConfig{
					Routers: []v1beta1.Router{{
						ASN:       65001,
						Neighbors: []v1beta1.Neighbor{n},
					}},
				},
			},
		})
	}
	return configs
}
