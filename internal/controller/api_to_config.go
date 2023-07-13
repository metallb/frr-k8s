// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"sort"

	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
	"k8s.io/apimachinery/pkg/util/sets"
)

func apiToFRR(fromK8s v1beta1.FRRConfiguration) (*frr.Config, error) {
	res := &frr.Config{
		Routers: make([]*frr.RouterConfig, 0),
		//BFDProfiles: sm.bfdProfiles,
		//ExtraConfig: sm.extraConfig,
	}

	for _, r := range fromK8s.Spec.BGP.Routers {
		frrRouter, err := routerToFRRConfig(r)
		if err != nil {
			return nil, err
		}
		res.Routers = append(res.Routers, frrRouter)
	}
	return res, nil
}
func routerToFRRConfig(r v1beta1.Router) (*frr.RouterConfig, error) {
	res := &frr.RouterConfig{
		MyASN:        r.ASN,
		RouterID:     r.ID,
		VRF:          r.VRF,
		Neighbors:    make([]*frr.NeighborConfig, 0),
		IPV4Prefixes: make([]string, 0),
		IPV6Prefixes: make([]string, 0),
	}

	for _, p := range r.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.IPV4Prefixes = append(res.IPV4Prefixes, p)
		case ipfamily.IPv6:
			res.IPV6Prefixes = append(res.IPV6Prefixes, p)
		case ipfamily.Unknown:
			return nil, fmt.Errorf("unknown ipfamily for %s", p)
		}
	}

	for _, n := range r.Neighbors {
		frrNeigh, err := neighborToFRR(n, res.IPV4Prefixes, res.IPV6Prefixes)
		if err != nil {
			return nil, err
		}
		res.Neighbors = append(res.Neighbors, frrNeigh)
	}

	return res, nil
}

func neighborToFRR(n v1beta1.Neighbor, ipv4Prefixes, ipv6Prefixes []string) (*frr.NeighborConfig, error) {
	neighborFamily, err := ipfamily.ForAddresses(n.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to find ipfamily for %s, %w", n.Address, err)
	}
	res := &frr.NeighborConfig{
		Name: neighborName(n.ASN, n.Address),
		ASN:  n.ASN,
		Addr: n.Address,
		Port: n.Port,
		// Password:       n.Password, TODO password as secret
		Advertisements: make([]*frr.AdvertisementConfig, 0),
		IPFamily:       neighborFamily,
		EBGPMultiHop:   n.EBGPMultiHop,
	}

	advs := map[string]*frr.AdvertisementConfig{}
	if n.ToAdvertise.Allowed.Mode == v1beta1.AllowAll {
		for _, p := range ipv4Prefixes {
			advs[p] = &frr.AdvertisementConfig{Prefix: p, IPFamily: ipfamily.IPv4}
			res.HasV4Advertisements = true
		}
		for _, p := range ipv6Prefixes {
			advs[p] = &frr.AdvertisementConfig{Prefix: p, IPFamily: ipfamily.IPv6}
			res.HasV6Advertisements = true
		}
	}

	for _, p := range n.ToAdvertise.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.HasV4Advertisements = true
		case ipfamily.IPv6:
			res.HasV6Advertisements = true
		}

		// TODO: check that the prefix matches the passed IPv4/IPv6 prefixes
		advs[p] = &frr.AdvertisementConfig{Prefix: p, IPFamily: family}
	}

	prefixToCommunities := map[string]sets.Set[string]{}
	for _, pfxs := range n.ToAdvertise.PrefixesWithCommunity {
		// TODO: add community format verification
		for _, p := range pfxs.Prefixes {
			_, ok := advs[p]
			if !ok {
				return nil, fmt.Errorf("prefix %s with community %s not in allowed list for neighbor %s", p, pfxs.Community, n.Address)
			}
			_, ok = prefixToCommunities[p]
			if !ok {
				prefixToCommunities[p] = sets.New(pfxs.Community)
				continue
			}

			prefixToCommunities[p].Insert(pfxs.Community)
		}
	}

	for p, c := range prefixToCommunities {
		adv := advs[p] // we don't check if the adv exists as it's done in the previous loop
		adv.Communities = sets.List(c)
	}

	res.Advertisements = sortMap(advs)

	return res, nil
}

func neighborName(ASN uint32, peerAddr string) string {
	return fmt.Sprintf("%d@%s", ASN, peerAddr)
}

func sortMap[T any](toSort map[string]T) []T {
	keys := make([]string, 0)
	for k := range toSort {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := make([]T, 0)
	for _, k := range keys {
		res = append(res, toSort[k])
	}
	return res
}
