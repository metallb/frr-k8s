// SPDX-License-Identifier:Apache-2.0

package config

import (
	"net"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"go.universe.tf/e2etest/pkg/frr"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
)

type Peer struct {
	IP    string
	Neigh frrk8sv1beta1.Neighbor
	FRR   frrcontainer.FRR
}

// PeersForContainers returns two lists of Peers, one for v4 addresses and one for v6 addresses.
func PeersForContainers(frrs []*frrcontainer.FRR, ipFam ipfamily.Family, modify ...func(frr.Neighbor)) ([]Peer, []Peer) {
	resV4 := make([]Peer, 0)
	resV6 := make([]Peer, 0)
	for _, f := range frrs {
		addresses := f.AddressesForFamily(ipFam)
		ebgpMultihop := false
		if f.NeighborConfig.MultiHop && f.NeighborConfig.ASN != f.RouterConfig.ASN {
			ebgpMultihop = true
		}

		for _, address := range addresses {
			peer := Peer{
				IP: address,
				Neigh: frrk8sv1beta1.Neighbor{
					ASN:          f.RouterConfig.ASN,
					Address:      address,
					Port:         f.RouterConfig.BGPPort,
					EBGPMultiHop: ebgpMultihop,
				},
				FRR: *f,
			}

			if ipfamily.ForAddress(net.ParseIP(address)) == ipfamily.IPv4 {
				resV4 = append(resV4, peer)
				continue
			}
			resV6 = append(resV6, peer)
		}
	}
	return resV4, resV6
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
