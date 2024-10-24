// SPDX-License-Identifier:Apache-2.0

package config

import (
	"fmt"
	"net"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frrk8stests/pkg/k8s"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Peer struct {
	IP    string
	Neigh frrk8sv1beta1.Neighbor
	FRR   frrcontainer.FRR
}

type PeersConfig struct {
	PeersV4 []Peer
	PeersV6 []Peer
	Secrets []corev1.Secret
}

func (pc PeersConfig) Peers() []Peer {
	return append(pc.PeersV4, pc.PeersV6...)
}

func PeersForContainers(frrs []*frrcontainer.FRR, ipFam ipfamily.Family, options ...func(*PeersConfig)) PeersConfig {
	res := PeersConfig{
		PeersV4: make([]Peer, 0),
		PeersV6: make([]Peer, 0),
		Secrets: make([]corev1.Secret, 0),
	}

	for _, f := range frrs {

		ebgpMultihop := false
		if f.NeighborConfig.MultiHop && f.NeighborConfig.ASN != f.RouterConfig.ASN {
			ebgpMultihop = true
		}

		addresses := f.AddressesForFamily(ipFam)
		for _, address := range addresses {
			peer := Peer{
				IP: address,
				Neigh: frrk8sv1beta1.Neighbor{
					ASN:          f.RouterConfig.ASN,
					Address:      address,
					Port:         &f.RouterConfig.BGPPort,
					EBGPMultiHop: ebgpMultihop,
				},
				FRR: *f,
			}

			if f.RouterConfig.Password != "" {
				s := corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      f.Name,
						Namespace: k8s.FRRK8sNamespace,
					},
					Type: corev1.SecretTypeBasicAuth,
					Data: map[string][]byte{
						"password": []byte(f.RouterConfig.Password),
					},
				}
				peer.Neigh.PasswordSecret = frrk8sv1beta1.SecretReference{
					Name:      f.Name,
					Namespace: k8s.FRRK8sNamespace,
				}
				res.Secrets = append(res.Secrets, s)
			}

			if ipfamily.ForAddress(net.ParseIP(address)) == ipfamily.IPv4 {
				res.PeersV4 = append(res.PeersV4, peer)
				continue
			}
			res.PeersV6 = append(res.PeersV6, peer)
		}
	}

	for _, option := range options {
		option(&res)
	}
	return res
}

func EnableSimpleBFD(pc *PeersConfig) {
	t := pc.PeersV4
	for i := 0; i < len(t); i++ {
		t[i].Neigh.BFDProfile = "simple"
	}
	t = pc.PeersV6
	for i := 0; i < len(t); i++ {
		t[i].Neigh.BFDProfile = "simple"
	}
}

func EnableGracefulRestart(pc *PeersConfig) {
	t := pc.PeersV4
	for i := 0; i < len(t); i++ {
		t[i].Neigh.EnableGracefulRestart = true
	}
	t = pc.PeersV6
	for i := 0; i < len(t); i++ {
		t[i].Neigh.EnableGracefulRestart = true
	}
}

func EnableAllowAll(pc *PeersConfig) {
	t := pc.PeersV4
	for i := 0; i < len(t); i++ {
		t[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
	}
	t = pc.PeersV6
	for i := 0; i < len(t); i++ {
		t[i].Neigh.ToAdvertise.Allowed.Mode = frrk8sv1beta1.AllowAll
	}
}

func NeighborsFromPeers(peers []Peer, peers1 []Peer) []frrk8sv1beta1.Neighbor {
	res := make([]frrk8sv1beta1.Neighbor, 0)
	for _, p := range peers {
		res = append(res, p.Neigh)
	}
	for _, p := range peers1 {
		res = append(res, p.Neigh)
	}
	return res
}

// ContainersForVRF filters the current list of FRR containers to only those
// that are configured for the given VRF.
func ContainersForVRF(frrs []*frrcontainer.FRR, vrf string) []*frrcontainer.FRR {
	res := make([]*frrcontainer.FRR, 0)
	for _, f := range frrs {
		if f.RouterConfig.VRF == vrf {
			res = append(res, f)
		}
	}
	return res
}

func ContainerByName(frrs []*frrcontainer.FRR, name string) (*frrcontainer.FRR, error) {
	for _, f := range frrs {
		if f.Name == name {
			return f, nil
		}
	}
	return nil, fmt.Errorf("container with name %s not found", name)
}
