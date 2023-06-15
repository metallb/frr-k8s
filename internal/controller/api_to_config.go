// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"

	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
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

	for _, n := range r.Neighbors {
		frrNeigh, err := neighborToFRR(n)
		if err != nil {
			return nil, err
		}
		res.Neighbors = append(res.Neighbors, frrNeigh)
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
	return res, nil
}

func neighborToFRR(n v1beta1.Neighbor) (*frr.NeighborConfig, error) {
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
	// TODO allow all
	for _, p := range n.ToAdvertise.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.HasV4Advertisements = true
		case ipfamily.IPv6:
			res.HasV6Advertisements = true
		}
		res.Advertisements = append(res.Advertisements, &frr.AdvertisementConfig{Prefix: p, IPFamily: family})
	}

	return res, nil
}

func neighborName(ASN uint32, peerAddr string) string {
	return fmt.Sprintf("%d@%s", ASN, peerAddr)
}
