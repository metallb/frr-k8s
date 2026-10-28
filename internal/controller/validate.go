// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"net"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TransientError is an error that happens due to interdependencies
// between crds, such as referencing non-existing secrets.
// Since we don't want webhooks to make assumptions on ordering, we reset the
// fields that could cause a transient error from configurations before validating them.
type TransientError struct {
	Message string
}

func (e TransientError) Error() string { return e.Message }

func Validate(resources ...client.ObjectList) error {
	clusterResources := ClusterResources{
		FRRConfigs: make([]v1beta1.FRRConfiguration, 0),
	}

	for _, list := range resources {
		if l, ok := list.(*v1beta1.FRRConfigurationList); ok {
			clusterResources.FRRConfigs = append(clusterResources.FRRConfigs, l.DeepCopy().Items...)
		}
	}
	resetSecrets(clusterResources.FRRConfigs)

	_, err := apiToFRR(clusterResources, []net.IPNet{})
	return err
}

// Resets secret references for validation, where their values are unknown.
// Each secret-only neighbor takes a known password from the same peer and VRF.
// Known passwords are left intact, preserving conflict checks.
// Secret contents, shared-secret consistency across peers, and mutually
// exclusive password sources are checked during reconciliation.
func resetSecrets(cfgs []v1beta1.FRRConfiguration) {
	type neighborKey struct {
		vrf string
		id  string
	}
	neighbors := map[neighborKey][]*v1beta1.Neighbor{}
	for _, cfg := range cfgs {
		for _, r := range cfg.Spec.BGP.Routers {
			for i := range r.Neighbors {
				n := &r.Neighbors[i]
				peer := frr.NeighborConfig{Addr: n.Address, Iface: n.Interface}
				key := neighborKey{vrf: r.VRF, id: peer.ID()}
				neighbors[key] = append(neighbors[key], n)
			}
		}
	}

	for _, peers := range neighbors {
		password := ""
		for _, n := range peers {
			if n.PasswordSecret.Name == "" || n.Password != "" {
				password = n.Password
				break
			}
		}
		for _, n := range peers {
			if n.PasswordSecret.Name == "" {
				continue
			}
			if n.Password == "" {
				n.Password = password
			}
			n.PasswordSecret = v1beta1.SecretReference{}
		}
	}
}
