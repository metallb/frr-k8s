// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"

	"github.com/metallb/frr-k8s/internal/frr"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	defaultBGPPort       = 179
	defaultHoldTime      = 180
	defaultKeepaliveTime = 60
	defaultConnectTime   = 60
)

// Merges two router configs.
func mergeRouterConfigs(r, toMerge *frr.RouterConfig) (*frr.RouterConfig, error) {
	err := routersAreCompatible(r, toMerge)
	if err != nil {
		return nil, err
	}

	if r.RouterID == "" {
		r.RouterID = toMerge.RouterID
	}

	v4Prefixes := sets.New(append(r.IPV4Prefixes, toMerge.IPV4Prefixes...)...)
	v6Prefixes := sets.New(append(r.IPV6Prefixes, toMerge.IPV6Prefixes...)...)
	importVRFs := sets.New(append(r.ImportVRFs, toMerge.ImportVRFs...)...)

	mergedNeighbors, err := mergeNeighbors(r.Neighbors, toMerge.Neighbors)
	if err != nil {
		return nil, err
	}

	r.IPV4Prefixes = sets.List(v4Prefixes)
	r.IPV6Prefixes = sets.List(v6Prefixes)
	r.ImportVRFs = sets.List(importVRFs)
	r.Neighbors = mergedNeighbors

	return r, nil
}

// Merges two neighbors slices corresponding to the same router.
func mergeNeighbors(curr, toMerge []*frr.NeighborConfig) ([]*frr.NeighborConfig, error) {
	all := curr
	all = append(all, toMerge...)
	if len(all) == 0 {
		return []*frr.NeighborConfig{}, nil
	}

	mergedNeighbors := map[string]*frr.NeighborConfig{}

	for _, n := range all {
		curr, found := mergedNeighbors[n.ID()]
		if !found {
			mergedNeighbors[n.ID()] = n
			continue
		}

		err := neighborsAreCompatible(curr, n)
		if err != nil {
			return nil, err
		}
		curr.Outgoing, err = mergeAllowedOut(curr.Outgoing, n.Outgoing)
		if err != nil {
			return nil, fmt.Errorf("could not merge outgoing for neighbor %s vrf %s, err: %w", n.Addr, n.VRFName, err)
		}

		curr.Incoming = mergeAllowedIn(curr.Incoming, n.Incoming)

		cleanNeighborDefaults(curr)
		mergedNeighbors[n.ID()] = curr
	}

	return sortMap(mergedNeighbors), nil
}

// Merges the allowed out prefixes, assuming they are for the same neighbor.
func mergeAllowedOut(r, toMerge frr.AllowedOut) (frr.AllowedOut, error) {
	mergedPrefixesV4 := sets.New(r.PrefixesV4...)
	mergedPrefixesV4.Insert(toMerge.PrefixesV4...)
	mergedPrefixesV6 := sets.New(r.PrefixesV6...)
	mergedPrefixesV6.Insert(toMerge.PrefixesV6...)

	res := frr.AllowedOut{
		PrefixesV4: sets.List(mergedPrefixesV4),
		PrefixesV6: sets.List(mergedPrefixesV6),
	}

	localPrefForPrefix := map[string]uint32{}
	for _, p := range r.LocalPrefPrefixesModifiers {
		for _, prefix := range p.Prefixes.UnsortedList() {
			localPrefForPrefix[prefix] = p.LocalPref
		}
	}
	for _, p := range toMerge.LocalPrefPrefixesModifiers {
		for _, prefix := range p.Prefixes.UnsortedList() {
			if existing, ok := localPrefForPrefix[prefix]; ok && existing != p.LocalPref {
				return frr.AllowedOut{}, fmt.Errorf("multiple local prefs (%d != %d) specified for prefix %s", existing, p.LocalPref, prefix)
			}
		}
	}

	res.CommunityPrefixesModifiers = mergeCommunityPrefixLists(r.CommunityPrefixesModifiers, toMerge.CommunityPrefixesModifiers)
	res.LocalPrefPrefixesModifiers = mergeLocalPrefPrefixLists(r.LocalPrefPrefixesModifiers, toMerge.LocalPrefPrefixesModifiers)

	return res, nil
}

func mergeLocalPrefPrefixLists(curr, toMerge []frr.LocalPrefPrefixList) []frr.LocalPrefPrefixList {
	allMap := map[string]frr.LocalPrefPrefixList{}
	for _, prefixList := range curr {
		allMap[localPrefPrefixListKey(prefixList.LocalPref, prefixList.IPFamily)] = prefixList
	}
	for _, prefixList := range toMerge {
		k := localPrefPrefixListKey(prefixList.LocalPref, prefixList.IPFamily)
		addTo, ok := allMap[k]
		if !ok {
			allMap[k] = prefixList
			continue
		}
		addTo.Prefixes = addTo.Prefixes.Union(prefixList.Prefixes)
		allMap[k] = addTo
	}

	return sortMap(allMap)
}

func mergeCommunityPrefixLists(curr, toMerge []frr.CommunityPrefixList) []frr.CommunityPrefixList {
	allMap := map[string]frr.CommunityPrefixList{}
	for _, prefixList := range curr {
		allMap[communityPrefixListKey(prefixList.Community, prefixList.IPFamily)] = prefixList
	}
	for _, prefixList := range toMerge {
		k := communityPrefixListKey(prefixList.Community, prefixList.IPFamily)
		addTo, ok := allMap[k]
		if !ok {
			allMap[k] = prefixList
			continue
		}
		addTo.Prefixes = addTo.Prefixes.Union(prefixList.Prefixes)
		allMap[k] = addTo
	}

	return sortMap(allMap)
}

// Merges the allowed incoming prefixes, assuming they are for the same neighbor.
func mergeAllowedIn(r, toMerge frr.AllowedIn) frr.AllowedIn {
	res := frr.AllowedIn{
		PrefixesV4: make([]frr.IncomingFilter, 0),
		PrefixesV6: make([]frr.IncomingFilter, 0),
	}
	if r.All || toMerge.All {
		res.All = true
		return res
	}

	res.PrefixesV4 = mergeIncomingFilters(r.PrefixesV4, toMerge.PrefixesV4)
	res.PrefixesV6 = mergeIncomingFilters(r.PrefixesV6, toMerge.PrefixesV6)

	return res
}

// cleanNeighborDefaults unset any field whose value that is equal to the default
// value for that field. This ensures consistency across conversions.
func cleanNeighborDefaults(neigh *frr.NeighborConfig) {
	if neigh.Port != nil && *neigh.Port == defaultBGPPort {
		neigh.Port = nil
	}
	if neigh.HoldTime != nil && *neigh.HoldTime == defaultHoldTime {
		neigh.HoldTime = nil
	}
	if neigh.KeepaliveTime != nil && *neigh.KeepaliveTime == defaultKeepaliveTime {
		neigh.KeepaliveTime = nil
	}
	if neigh.ConnectTime != nil && *neigh.ConnectTime == defaultConnectTime {
		neigh.ConnectTime = nil
	}
}

func mergeIncomingFilters(curr, toMerge []frr.IncomingFilter) []frr.IncomingFilter {
	all := curr
	all = append(all, toMerge...)
	if len(all) == 0 {
		return []frr.IncomingFilter{}
	}

	mergedIn := map[string]*frr.IncomingFilter{}
	for _, a := range all {
		f := a
		key := fmt.Sprintf("%s%d%d", f.Prefix, f.LE, f.GE)
		mergedIn[key] = &f
	}

	return sortMapPtr(mergedIn)
}

// Verifies that two routers are compatible for merging.
func routersAreCompatible(r, toMerge *frr.RouterConfig) error {
	if r.VRF != toMerge.VRF {
		return fmt.Errorf("different VRFs specified (%s != %s)", r.VRF, toMerge.VRF)
	}

	if r.MyASN != toMerge.MyASN {
		return fmt.Errorf("different asns (%d != %d) specified for same vrf: %s", r.MyASN, toMerge.MyASN, r.VRF)
	}

	bothRouterIDsNonEmpty := r.RouterID != "" && toMerge.RouterID != ""
	routerIDsDifferent := r.RouterID != toMerge.RouterID
	if bothRouterIDsNonEmpty && routerIDsDifferent {
		return fmt.Errorf("different router ids (%s != %s) specified for same vrf: %s", r.RouterID, toMerge.RouterID, r.VRF)
	}

	return nil
}

// Verifies that two neighbors are compatible for merging, assuming they belong to the same router.
func neighborsAreCompatible(n1, n2 *frr.NeighborConfig) error {
	// we shouldn't reach this
	if n1.ID() != n2.ID() {
		return fmt.Errorf("only neighbors with same ID (%s, %s) are compatible for merging", n1.ID(), n2.ID())
	}

	neighborKey := n1.ID()
	if n1.ASN != n2.ASN {
		return fmt.Errorf("multiple asns specified for %s", neighborKey)
	}

	if !ptrsEqual(n1.Port, n2.Port, defaultBGPPort) {
		return fmt.Errorf("multiple ports specified for %s", neighborKey)
	}

	if n1.SrcAddr != n2.SrcAddr {
		return fmt.Errorf("multiple source addresses specified for %s", neighborKey)
	}

	if n1.Password != n2.Password {
		return fmt.Errorf("multiple passwords specified for %s", neighborKey)
	}

	if n1.BFDProfile != n2.BFDProfile {
		return fmt.Errorf("multiple bfd profiles specified for %s", neighborKey)
	}

	if n1.EBGPMultiHop != n2.EBGPMultiHop {
		return fmt.Errorf("conflicting ebgp-multihop specified for %s", neighborKey)
	}

	if n1.IPFamily != n2.IPFamily {
		return fmt.Errorf("conflicting advertiseDualStack specified for %s", neighborKey)
	}

	if !ptrsEqual(n1.HoldTime, n2.HoldTime, defaultHoldTime) {
		return fmt.Errorf("multiple hold times specified for %s", neighborKey)
	}

	if !ptrsEqual(n1.KeepaliveTime, n2.KeepaliveTime, defaultKeepaliveTime) {
		return fmt.Errorf("multiple keepalive times specified for %s", neighborKey)
	}

	if !ptrsEqual(n1.ConnectTime, n2.ConnectTime, defaultConnectTime) {
		return fmt.Errorf("multiple connect times specified for %s", neighborKey)
	}

	return nil
}

func ptrsEqual[T comparable](p1, p2 *T, def T) bool {
	if p1 == nil && p2 == nil {
		return true
	}

	if p1 == nil {
		return *p2 == def
	}

	if p2 == nil {
		return *p1 == def
	}

	return *p1 == *p2
}
