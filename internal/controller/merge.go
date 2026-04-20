// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"slices"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
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

	mergedNeighbors, err := mergeNeighborsLists(r.Neighbors, toMerge.Neighbors)
	if err != nil {
		return nil, err
	}

	mergedEVPN, err := mergeEVPNConfigs(r.EVPN, toMerge.EVPN)
	if err != nil {
		return nil, fmt.Errorf("could not merge EVPN configuration for vrf %q, err: %w", r.VRF, err)
	}

	r.IPV4Prefixes = sets.List(v4Prefixes)
	r.IPV6Prefixes = sets.List(v6Prefixes)
	r.ImportVRFs = sets.List(importVRFs)
	r.Neighbors = mergedNeighbors
	r.EVPN = mergedEVPN

	return r, nil
}

// mergeNeighborsLists merges two neighbor configuration slices corresponding to the same router.
// It combines both slices and merges neighbors with the same ID (address+VRF combination).
// Returns a sorted list of merged neighbor configurations or an error if neighbors are incompatible.
func mergeNeighborsLists(first, toMerge []*frr.NeighborConfig) ([]*frr.NeighborConfig, error) {
	all := slices.Concat(first, toMerge)
	if len(all) == 0 {
		return []*frr.NeighborConfig{}, nil
	}

	mergedNeighbors := map[string]*frr.NeighborConfig{}
	for _, n := range all {
		id := n.ID()
		if _, found := mergedNeighbors[id]; !found {
			mergedNeighbors[id] = n
			continue
		}
		if err := mergeIntoNeighbor(mergedNeighbors[id], n); err != nil {
			return nil, err
		}
	}

	return sortMap(mergedNeighbors), nil
}

// mergeIntoNeighbor merges the source neighbor configuration into the destination neighbor.
// Returns an error if the neighbors have incompatible configurations or if an error occurs while merging.
func mergeIntoNeighbor(dest, src *frr.NeighborConfig) error {
	err := neighborsAreCompatible(dest, src)
	if err != nil {
		return err
	}

	if dest.BFDProfile == "" {
		dest.BFDProfile = src.BFDProfile
	}

	dest.Outgoing, err = mergeAllowedOut(dest.Outgoing, src.Outgoing)
	if err != nil {
		return fmt.Errorf("could not merge outgoing for neighbor %s vrf %s, err: %w", src.Addr, src.VRFName, err)
	}
	dest.Incoming = mergeAllowedIn(dest.Incoming, src.Incoming)
	dest.AddressFamilies = sets.List(sets.New(append(dest.AddressFamilies, src.AddressFamilies...)...))

	cleanNeighborDefaults(dest)

	return nil
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

	// Configurations are compatible if at least one of the BFDProfiles is empty, or if they match.
	if n1.BFDProfile != "" && n2.BFDProfile != "" && n1.BFDProfile != n2.BFDProfile {
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

	if n1.LocalASN != n2.LocalASN {
		return fmt.Errorf("multiple localASNs specified for %s", neighborKey)
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

func mergeEVPNConfigs(a, b *frr.EVPNConfig) (*frr.EVPNConfig, error) {
	if a == nil && b == nil {
		return nil, nil
	}
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}

	if !ptrsEqual(a.AdvertiseVNIs, b.AdvertiseVNIs, string(v1beta1.VNIAdvertisementDisabled)) {
		return nil, fmt.Errorf("different advertiseVNIs (%q != %q)", ptr.Deref(a.AdvertiseVNIs, string(v1beta1.VNIAdvertisementDisabled)), ptr.Deref(b.AdvertiseVNIs, string(v1beta1.VNIAdvertisementDisabled)))
	}
	if a.AdvertiseSVI != b.AdvertiseSVI {
		return nil, fmt.Errorf("different advertiseSVI (%t != %t)", a.AdvertiseSVI, b.AdvertiseSVI)
	}

	mergedL2VNIs, err := mergeL2VNIs(a.L2VNIs, b.L2VNIs)
	if err != nil {
		return nil, err
	}

	mergedL3VNI, err := mergeL3VNIs(a.L3VNI, b.L3VNI)
	if err != nil {
		return nil, err
	}

	res := &frr.EVPNConfig{
		AdvertiseVNIs: a.AdvertiseVNIs,
		AdvertiseSVI:  a.AdvertiseSVI,
		L2VNIs:        mergedL2VNIs,
		L3VNI:         mergedL3VNI,
	}
	if res.AdvertiseVNIs == nil {
		res.AdvertiseVNIs = b.AdvertiseVNIs
	}

	return res, nil
}

func mergeL2VNIs(a, b []frr.L2VNI) ([]frr.L2VNI, error) {
	if len(a) == 0 && len(b) == 0 {
		return nil, nil
	}
	if len(a) == 0 {
		return b, nil
	}
	if len(b) == 0 {
		return a, nil
	}

	byVNI := map[uint32]frr.L2VNI{}
	for _, v := range a {
		byVNI[v.VNI] = v
	}
	for _, v := range b {
		existing, found := byVNI[v.VNI]
		if !found {
			byVNI[v.VNI] = v
			continue
		}
		merged, err := mergeVNIProperties(existing.VNIProperties, v.VNIProperties)
		if err != nil {
			return nil, fmt.Errorf("could not merge l2vni %d, err: %w", existing.VNI, err)
		}
		byVNI[v.VNI] = frr.L2VNI{VNI: existing.VNI, VNIProperties: merged}
	}

	return sortMap(byVNI), nil
}

func mergeL3VNIs(a, b *frr.L3VNI) (*frr.L3VNI, error) {
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}
	if a.VNI != b.VNI {
		return nil, fmt.Errorf("different l3vni numbers (%d != %d)", a.VNI, b.VNI)
	}

	merged, err := mergeVNIProperties(a.VNIProperties, b.VNIProperties)
	if err != nil {
		return nil, err
	}

	advertisePrefixes := sets.New(append(a.AdvertisePrefixes, b.AdvertisePrefixes...)...)
	return &frr.L3VNI{
		VNI:               a.VNI,
		VNIProperties:     merged,
		AdvertisePrefixes: sets.List(advertisePrefixes),
	}, nil
}

// mergeVNIProperties merges the common VNI properties (RD, ImportRTs, ExportRTs).
// RD must be equal or one must be empty. Route targets are merged, but if one
// side omits them (relying on FRR auto) while the other specifies them, that's
// a conflict.
func mergeVNIProperties(a, b frr.VNIProperties) (frr.VNIProperties, error) {
	if a.RD != "" && b.RD != "" && a.RD != b.RD {
		return frr.VNIProperties{}, fmt.Errorf("different RD values (%s != %s)", a.RD, b.RD)
	}

	if (len(a.ImportRTs) == 0) != (len(b.ImportRTs) == 0) {
		return frr.VNIProperties{}, fmt.Errorf("conflicting import route targets: mixing implicit and explicit route targets")
	}
	if (len(a.ExportRTs) == 0) != (len(b.ExportRTs) == 0) {
		return frr.VNIProperties{}, fmt.Errorf("conflicting export route targets: mixing implicit and explicit route targets")
	}

	rd := a.RD
	if rd == "" {
		rd = b.RD
	}

	importRTs := mergeRTs(a.ImportRTs, b.ImportRTs)
	exportRTs := mergeRTs(a.ExportRTs, b.ExportRTs)

	return frr.VNIProperties{
		RD:        rd,
		ImportRTs: importRTs,
		ExportRTs: exportRTs,
	}, nil
}

func mergeRTs(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	merged := sets.New(append(a, b...)...)
	return sets.List(merged)
}
