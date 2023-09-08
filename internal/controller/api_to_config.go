// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"

	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/community"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type SecretNotFoundError struct {
	Name     string
	Neighbor string
}

func (s SecretNotFoundError) Error() string {
	return fmt.Sprintf("secret %s not found for neighbor %s", s.Name, s.Neighbor)
}

type namedRawConfig struct {
	v1beta1.RawConfig
	configName string
}

func apiToFRR(fromK8s []v1beta1.FRRConfiguration, secrets map[string]corev1.Secret) (*frr.Config, error) {
	res := &frr.Config{
		Routers:     make([]*frr.RouterConfig, 0),
		BFDProfiles: make([]frr.BFDProfile, 0),
	}

	rawConfigs := make([]namedRawConfig, 0)
	routersForVRF := map[string]*frr.RouterConfig{}
	bfdProfilesAllConfigs := map[string]*frr.BFDProfile{}
	for _, cfg := range fromK8s {
		bfdProfiles := map[string]*frr.BFDProfile{}
		if cfg.Spec.Raw.Config != "" {
			raw := namedRawConfig{RawConfig: cfg.Spec.Raw, configName: cfg.Name}
			rawConfigs = append(rawConfigs, raw)
		}

		for _, b := range cfg.Spec.BGP.BFDProfiles {
			frrBFDProfile := bfdProfileToFRR(b)
			// Handling profiles local to the current config
			if _, found := bfdProfiles[frrBFDProfile.Name]; found {
				return nil, fmt.Errorf("duplicate bfd profile name %s in config %s", frrBFDProfile.Name, cfg.Name)
			}
			bfdProfiles[frrBFDProfile.Name] = frrBFDProfile

			// Checking that profiles named after the same name in different configs carry the same
			// values
			old, found := bfdProfilesAllConfigs[frrBFDProfile.Name]
			if found && !reflect.DeepEqual(old, frrBFDProfile) {
				return nil, fmt.Errorf("duplicate bfd profile name %s with different values for config %s", frrBFDProfile.Name, cfg.Name)
			}

			if !found {
				bfdProfilesAllConfigs[frrBFDProfile.Name] = frrBFDProfile
			}
		}

		for _, r := range cfg.Spec.BGP.Routers {
			routerCfg, err := routerToFRRConfig(r, secrets, bfdProfiles)
			if err != nil {
				return nil, err
			}

			curr, ok := routersForVRF[r.VRF]
			if !ok {
				routersForVRF[r.VRF] = routerCfg
				continue
			}

			curr, err = mergeRouterConfigs(curr, routerCfg)
			if err != nil {
				return nil, err
			}

			routersForVRF[r.VRF] = curr
		}
	}

	res.Routers = sortMapPtr(routersForVRF)
	res.ExtraConfig = joinRawConfigs(rawConfigs)
	res.BFDProfiles = sortMap(bfdProfilesAllConfigs)

	return res, nil
}

func routerToFRRConfig(r v1beta1.Router, secrets map[string]corev1.Secret, bfdProfiles map[string]*frr.BFDProfile) (*frr.RouterConfig, error) {
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
		frrNeigh, err := neighborToFRR(n, res.IPV4Prefixes, res.IPV6Prefixes, secrets, bfdProfiles)
		if err != nil {
			return nil, fmt.Errorf("failed to process neighbor %s for router %d-%s: %w", neighborName(n.ASN, n.Address), r.ASN, r.VRF, err)
		}
		res.Neighbors = append(res.Neighbors, frrNeigh)
	}

	return res, nil
}

func neighborToFRR(n v1beta1.Neighbor, ipv4Prefixes, ipv6Prefixes []string, passwordSecrets map[string]corev1.Secret, bfdProfiles map[string]*frr.BFDProfile) (*frr.NeighborConfig, error) {
	neighborFamily, err := ipfamily.ForAddresses(n.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to find ipfamily for %s, %w", n.Address, err)
	}
	if _, ok := bfdProfiles[n.BFDProfile]; n.BFDProfile != "" && !ok {
		return nil, fmt.Errorf("neighbor %s referencing non existing BFDProfile %s", neighborName(n.ASN, n.Address), n.BFDProfile)
	}
	res := &frr.NeighborConfig{
		Name:         neighborName(n.ASN, n.Address),
		ASN:          n.ASN,
		Addr:         n.Address,
		Port:         n.Port,
		IPFamily:     neighborFamily,
		EBGPMultiHop: n.EBGPMultiHop,
		BFDProfile:   n.BFDProfile,
	}

	res.Password, err = passwordForNeighbor(n, passwordSecrets)
	if err != nil {
		return nil, err
	}
	res.Outgoing, err = toAdvertiseToFRR(n.ToAdvertise, ipv4Prefixes, ipv6Prefixes)
	if err != nil {
		return nil, err
	}
	res.Incoming = toReceiveToFRR(n.ToReceive)
	return res, nil
}

func passwordForNeighbor(n v1beta1.Neighbor, passwordSecrets map[string]corev1.Secret) (string, error) {
	if n.PasswordSecret.Name == "" {
		return "", nil
	}
	secret, ok := passwordSecrets[n.PasswordSecret.Name]
	if !ok {
		return "", SecretNotFoundError{Name: n.PasswordSecret.Name, Neighbor: neighborName(n.ASN, n.Address)}
	}
	if secret.Type != corev1.SecretTypeBasicAuth {
		return "", fmt.Errorf("secret type mismatch on %q/%q, type %q is expected ", secret.Namespace,
			secret.Name, corev1.SecretTypeBasicAuth)
	}
	srcPass, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("password field not specified in the secret %q/%q", secret.Namespace, secret.Name)
	}
	return string(srcPass), nil
}

func toAdvertiseToFRR(toAdvertise v1beta1.Advertise, ipv4Prefixes, ipv6Prefixes []string) (frr.AllowedOut, error) {
	advsV4, advsV6 := prefixesToMap(toAdvertise, ipv4Prefixes, ipv6Prefixes)
	communities, err := communityPrefixesToMap(toAdvertise.PrefixesWithCommunity)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	err = setCommunitiesToAdvertisements(advsV4, communities, ipfamily.IPv4)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	err = setCommunitiesToAdvertisements(advsV6, communities, ipfamily.IPv6)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	localPrefs, err := localPrefPrefixesToMap(toAdvertise.PrefixesWithLocalPref)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	err = setLocalPrefToAdvertisements(advsV4, localPrefs, ipfamily.IPv4)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	err = setLocalPrefToAdvertisements(advsV6, localPrefs, ipfamily.IPv6)
	if err != nil {
		return frr.AllowedOut{}, err
	}
	res := frr.AllowedOut{
		PrefixesV4: sortMap(advsV4),
		PrefixesV6: sortMap(advsV6),
	}
	return res, nil
}

// prefixesToMap returns two maps of prefix->OutgoingFIlter (ie family, advertisement, communities), one for each family.
func prefixesToMap(toAdvertise v1beta1.Advertise, ipv4Prefixes, ipv6Prefixes []string) (map[string]*frr.OutgoingFilter, map[string]*frr.OutgoingFilter) {
	resV4 := map[string]*frr.OutgoingFilter{}
	resV6 := map[string]*frr.OutgoingFilter{}
	if toAdvertise.Allowed.Mode == v1beta1.AllowAll {
		for _, p := range ipv4Prefixes {
			resV4[p] = &frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv4}
		}
		for _, p := range ipv6Prefixes {
			resV6[p] = &frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv6}
		}
		return resV4, resV6
	}
	// TODO: add a validation somewhere that checks that the prefixes are present in the
	// global per router list.
	for _, p := range toAdvertise.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			resV4[p] = &frr.OutgoingFilter{Prefix: p, IPFamily: family}
		case ipfamily.IPv6:
			resV6[p] = &frr.OutgoingFilter{Prefix: p, IPFamily: family}
		}
	}
	return resV4, resV6
}

// setCommunitiesToAdvertisements takes the given communityPrefixes and fills the relevant fields to the advertisements contained in the advs map.
func setCommunitiesToAdvertisements(advs map[string]*frr.OutgoingFilter, communities communityPrefixes, ipFamily ipfamily.Family) error {
	communitiesForPrefix := communities.communitiesForPrefixV4
	largeCommunitiesForPrefix := communities.largeCommunitiesForPrefixV4
	if ipFamily == ipfamily.IPv6 {
		communitiesForPrefix = communities.communitiesForPrefixV6
		largeCommunitiesForPrefix = communities.largeCommunitiesForPrefixV6
	}
	for p, c := range communitiesForPrefix {
		adv, ok := advs[p]
		if !ok {
			return fmt.Errorf("community associated to non existing prefix %s", p)
		}
		adv.Communities = sets.List(c)
	}

	for p, c := range largeCommunitiesForPrefix {
		adv, ok := advs[p]
		if !ok {
			return fmt.Errorf("large community associated to non existing prefix %s", p)
		}
		adv.LargeCommunities = sets.List(c)
	}
	return nil
}

// setLocalPrefToAdvertisements takes the given localPrefPrefixes and fills the relevant fields to the advertisements contained in the advs map.
func setLocalPrefToAdvertisements(advs map[string]*frr.OutgoingFilter, localPrefs localPrefPrefixes, ipFamily ipfamily.Family) error {
	localPrefsForPrefix := localPrefs.localPrefForPrefixV4
	if ipFamily == ipfamily.IPv6 {
		localPrefsForPrefix = localPrefs.localPrefForPrefixV6
	}

	for p, lp := range localPrefsForPrefix {
		adv, ok := advs[p]
		if !ok {
			return fmt.Errorf("localPref associated to non existing prefix %s", p)
		}
		adv.LocalPref = lp
	}

	return nil
}

func toReceiveToFRR(toReceive v1beta1.Receive) frr.AllowedIn {
	res := frr.AllowedIn{
		PrefixesV4: make([]frr.IncomingFilter, 0),
		PrefixesV6: make([]frr.IncomingFilter, 0),
	}
	if toReceive.Allowed.Mode == v1beta1.AllowAll {
		res.All = true
		return res
	}
	for _, p := range toReceive.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		if family == ipfamily.IPv4 {
			res.PrefixesV4 = append(res.PrefixesV4, frr.IncomingFilter{Prefix: p, IPFamily: family})
			continue
		}
		res.PrefixesV6 = append(res.PrefixesV6, frr.IncomingFilter{Prefix: p, IPFamily: family})
	}
	sort.Slice(res.PrefixesV4, func(i, j int) bool {
		return res.PrefixesV4[i].Prefix < res.PrefixesV4[j].Prefix
	})
	sort.Slice(res.PrefixesV6, func(i, j int) bool {
		return res.PrefixesV6[i].Prefix < res.PrefixesV6[j].Prefix
	})
	return res
}

func neighborName(ASN uint32, peerAddr string) string {
	return fmt.Sprintf("%d@%s", ASN, peerAddr)
}

type communityPrefixes struct {
	communitiesForPrefixV4      map[string]sets.Set[string]
	largeCommunitiesForPrefixV4 map[string]sets.Set[string]
	communitiesForPrefixV6      map[string]sets.Set[string]
	largeCommunitiesForPrefixV6 map[string]sets.Set[string]
}

func (c *communityPrefixes) mapFor(family ipfamily.Family, isLarge bool) map[string]sets.Set[string] {
	switch family {
	case ipfamily.IPv4:
		if isLarge {
			return c.largeCommunitiesForPrefixV4
		}
		return c.communitiesForPrefixV4
	case ipfamily.IPv6:
		if isLarge {
			return c.largeCommunitiesForPrefixV6
		}
		return c.communitiesForPrefixV6
	}
	return nil
}

func communityPrefixesToMap(withCommunity []v1beta1.CommunityPrefixes) (communityPrefixes, error) {
	res := communityPrefixes{
		communitiesForPrefixV4:      map[string]sets.Set[string]{},
		largeCommunitiesForPrefixV4: map[string]sets.Set[string]{},
		communitiesForPrefixV6:      map[string]sets.Set[string]{},
		largeCommunitiesForPrefixV6: map[string]sets.Set[string]{},
	}

	for _, pfxs := range withCommunity {
		c, err := community.New(pfxs.Community)
		if err != nil {
			return communityPrefixes{}, fmt.Errorf("invalid community %s, err: %w", pfxs.Community, err)
		}
		isLarge := community.IsLarge(c)
		for _, p := range pfxs.Prefixes {
			family := ipfamily.ForCIDRString(p)
			communityMap := res.mapFor(family, isLarge)
			_, ok := communityMap[p]
			if !ok {
				communityMap[p] = sets.New(c.String())
				continue
			}

			communityMap[p].Insert(c.String())
		}
	}
	return res, nil
}

type localPrefPrefixes struct {
	localPrefForPrefixV4 map[string]uint32
	localPrefForPrefixV6 map[string]uint32
}

func localPrefPrefixesToMap(withLocalPref []v1beta1.LocalPrefPrefixes) (localPrefPrefixes, error) {
	res := localPrefPrefixes{
		localPrefForPrefixV4: map[string]uint32{},
		localPrefForPrefixV6: map[string]uint32{},
	}

	for _, pfxs := range withLocalPref {
		for _, p := range pfxs.Prefixes {
			family := ipfamily.ForCIDRString(p)
			lpMap := res.localPrefForPrefixV4
			if family == ipfamily.IPv6 {
				lpMap = res.localPrefForPrefixV6
			}

			_, ok := lpMap[p]
			if ok {
				return localPrefPrefixes{}, fmt.Errorf("multiple local prefs specified for prefix %s", p)
			}

			lpMap[p] = pfxs.LocalPref
		}
	}

	return res, nil
}

func bfdProfileToFRR(bfdProfile v1beta1.BFDProfile) *frr.BFDProfile {
	return &frr.BFDProfile{
		Name:             bfdProfile.Name,
		ReceiveInterval:  pointerForValue(bfdProfile.ReceiveInterval),
		TransmitInterval: pointerForValue(bfdProfile.TransmitInterval),
		DetectMultiplier: pointerForValue(bfdProfile.DetectMultiplier),
		EchoInterval:     pointerForValue(bfdProfile.EchoInterval),
		EchoMode:         bfdProfile.EchoMode,
		PassiveMode:      bfdProfile.PassiveMode,
		MinimumTTL:       pointerForValue(bfdProfile.MinimumTTL),
	}
}

func pointerForValue(value uint32) *uint32 {
	if value != 0 {
		return &value
	}
	return nil
}

func sortMap[T any](toSort map[string]*T) []T {
	keys := make([]string, 0)
	for k := range toSort {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := make([]T, 0)
	for _, k := range keys {
		res = append(res, *toSort[k])
	}
	return res
}

func sortMapPtr[T any](toSort map[string]*T) []*T {
	keys := make([]string, 0)
	for k := range toSort {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := make([]*T, 0)
	for _, k := range keys {
		res = append(res, toSort[k])
	}
	return res
}

func joinRawConfigs(raw []namedRawConfig) string {
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Priority == raw[j].Priority {
			return raw[i].configName < raw[j].configName
		}
		return raw[i].Priority < raw[j].Priority
	})
	res := bytes.Buffer{}
	for _, r := range raw {
		res.Write([]byte(r.Config))
		res.WriteString("\n")
	}
	return res.String()
}
