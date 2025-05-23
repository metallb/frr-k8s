// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"time"

	v1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/community"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"github.com/metallb/frr-k8s/internal/safeconvert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
)

type ClusterResources struct {
	FRRConfigs      []v1beta1.FRRConfiguration
	PasswordSecrets map[string]corev1.Secret
}

type namedRawConfig struct {
	v1beta1.RawConfig
	configName string
}

func apiToFRR(resources ClusterResources, alwaysBlock []net.IPNet) (*frr.Config, error) {
	res := &frr.Config{
		Routers:     make([]*frr.RouterConfig, 0),
		BFDProfiles: make([]frr.BFDProfile, 0),
	}

	rawConfigs := make([]namedRawConfig, 0)
	routersForVRF := map[string]*frr.RouterConfig{}
	bfdProfilesAllConfigs := map[string]*frr.BFDProfile{}
	for _, cfg := range resources.FRRConfigs {
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

		alwaysBlockFRR := alwaysBlockToFRR(alwaysBlock)
		routersPrefixes := prefixesForVRFs(cfg.Spec.BGP.Routers)

		for _, r := range cfg.Spec.BGP.Routers {
			if err := validatePrefixes(r.Prefixes); err != nil {
				return nil, err
			}

			if err := validateImportVRFs(r, routersPrefixes); err != nil {
				return nil, err
			}

			allPrefixes := make([]string, len(r.Prefixes))
			copy(allPrefixes, r.Prefixes)

			importedPrefixes, err := importedPrefixes(r, routersPrefixes)
			if err != nil {
				return nil, err
			}
			allPrefixes = append(allPrefixes, importedPrefixes...)

			if err := validateOutgoingPrefixes(allPrefixes, r); err != nil {
				return nil, err
			}

			routerCfg, err := routerToFRRConfig(r, alwaysBlockFRR, resources.PasswordSecrets, bfdProfiles, allPrefixes)
			if err != nil {
				return nil, err
			}

			if err := validateRouterConfig(routerCfg); err != nil {
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

func routerToFRRConfig(r v1beta1.Router, alwaysBlock []frr.IncomingFilter, secrets map[string]corev1.Secret, bfdProfiles map[string]*frr.BFDProfile, routerPrefixes []string) (*frr.RouterConfig, error) {
	res := &frr.RouterConfig{
		MyASN:        r.ASN,
		RouterID:     r.ID,
		VRF:          r.VRF,
		Neighbors:    make([]*frr.NeighborConfig, 0),
		IPV4Prefixes: ipfamily.FilterPrefixes(r.Prefixes, ipfamily.IPv4),
		IPV6Prefixes: ipfamily.FilterPrefixes(r.Prefixes, ipfamily.IPv6),
		ImportVRFs:   make([]string, 0),
	}

	for _, n := range r.Neighbors {
		frrNeigh, err := neighborToFRR(n, routerPrefixes, alwaysBlock, r.VRF, secrets, bfdProfiles)
		if err != nil {
			return nil, fmt.Errorf("failed to process neighbor %s for router %d-%s: %w", neighborName(n), r.ASN, r.VRF, err)
		}
		res.Neighbors = append(res.Neighbors, frrNeigh)
	}

	for _, v := range r.Imports {
		res.ImportVRFs = append(res.ImportVRFs, v.VRF)
	}
	return res, nil
}

func neighborToFRR(n v1beta1.Neighbor, prefixesInRouter []string, alwaysBlock []frr.IncomingFilter, routerVRF string, passwordSecrets map[string]corev1.Secret, bfdProfiles map[string]*frr.BFDProfile) (*frr.NeighborConfig, error) {
	if n.Address == "" && n.Interface == "" {
		return nil, fmt.Errorf("neighbor with ASN %s has no address and no interface", asnFor(n))
	}

	if n.Address != "" && n.Interface != "" {
		return nil, fmt.Errorf("neighbor %s has both Address and Interface specified", neighborName(n))
	}

	neighborFamily := ipfamily.Unknown
	if n.Address != "" {
		f, err := ipfamily.ForAddresses(n.Address)
		if err != nil {
			return nil, fmt.Errorf("failed to find ipfamily for %s, %w", n.Address, err)
		}
		neighborFamily = f
	}
	if _, ok := bfdProfiles[n.BFDProfile]; n.BFDProfile != "" && !ok {
		return nil, fmt.Errorf("neighbor %s referencing non existing BFDProfile %s", neighborName(n), n.BFDProfile)
	}

	if n.ASN == 0 && n.DynamicASN == "" {
		return nil, fmt.Errorf("neighbor %s has no ASN or DynamicASN specified", neighborName(n))
	}

	if n.ASN != 0 && n.DynamicASN != "" {
		return nil, fmt.Errorf("neighbor %s has both ASN and DynamicASN specified", neighborName(n))
	}

	if n.DynamicASN != "" && n.DynamicASN != v1beta1.InternalASNMode && n.DynamicASN != v1beta1.ExternalASNMode {
		return nil, fmt.Errorf("neighbor %s has invalid DynamicASN %s specified, must be one of %s,%s", neighborName(n), n.DynamicASN, v1beta1.InternalASNMode, v1beta1.ExternalASNMode)
	}

	address := n.Address
	if n.Interface != "" || n.DualStackAddressFamily {
		neighborFamily = ipfamily.DualStack
	}

	res := &frr.NeighborConfig{
		Name:            neighborName(n),
		ASN:             asnFor(n),
		SrcAddr:         n.SourceAddress,
		Addr:            address,
		Iface:           n.Interface,
		Port:            n.Port,
		IPFamily:        neighborFamily,
		EBGPMultiHop:    n.EBGPMultiHop,
		BFDProfile:      n.BFDProfile,
		GracefulRestart: n.EnableGracefulRestart,
		VRFName:         routerVRF,
		AlwaysBlock:     alwaysBlock,
	}

	var err error
	res.HoldTime, res.KeepaliveTime, err = parseTimers(n.HoldTime, n.KeepaliveTime)
	if err != nil {
		return nil, fmt.Errorf("invalid timers for neighbor %s, err: %w", neighborName(n), err)
	}

	if n.ConnectTime != nil {
		res.ConnectTime = ptr.To(int64(n.ConnectTime.Duration / time.Second))
	}

	res.Password, err = passwordForNeighbor(n, passwordSecrets)
	if err != nil {
		return nil, err
	}
	res.Outgoing, err = toAdvertiseToFRR(n.ToAdvertise, prefixesInRouter)
	if err != nil {
		return nil, err
	}
	res.Incoming, err = toReceiveToFRR(n.ToReceive)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func passwordForNeighbor(n v1beta1.Neighbor, passwordSecrets map[string]corev1.Secret) (string, error) {
	if n.Password != "" && n.PasswordSecret.Name != "" {
		return "", fmt.Errorf("neighbor %s specifies both cleartext password and secret ref", neighborName(n))
	}

	if n.Password != "" {
		return n.Password, nil
	}

	if n.PasswordSecret.Name == "" {
		return "", nil
	}

	secret, ok := passwordSecrets[n.PasswordSecret.Name]
	if !ok {
		return "", TransientError{Message: fmt.Sprintf("secret %s not found for neighbor %s", n.PasswordSecret.Name, neighborName(n))}
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

func toAdvertiseToFRR(toAdvertise v1beta1.Advertise, prefixesInRouter []string) (frr.AllowedOut, error) {
	ipv4Prefixes := ipfamily.FilterPrefixes(prefixesInRouter, ipfamily.IPv4)
	ipv6Prefixes := ipfamily.FilterPrefixes(prefixesInRouter, ipfamily.IPv6)
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

// prefixesToMap returns two maps of prefix->OutgoingFilter (ie family, advertisement, communities), one for each family.
// The ipv4Prefixes and ipv6Prefixes represent the "global" allowed prefixes which are the prefixes defined on the router.
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

func toReceiveToFRR(toReceive v1beta1.Receive) (frr.AllowedIn, error) {
	res := frr.AllowedIn{
		PrefixesV4: make([]frr.IncomingFilter, 0),
		PrefixesV6: make([]frr.IncomingFilter, 0),
	}
	if toReceive.Allowed.Mode == v1beta1.AllowAll {
		res.All = true
		return res, nil
	}
	for _, s := range toReceive.Allowed.Prefixes {
		filter, err := filterForSelector(s)
		if err != nil {
			return frr.AllowedIn{}, err
		}
		if filter.IPFamily == ipfamily.IPv4 {
			res.PrefixesV4 = append(res.PrefixesV4, filter)
			continue
		}
		res.PrefixesV6 = append(res.PrefixesV6, filter)
	}
	sort.Slice(res.PrefixesV4, func(i, j int) bool {
		return res.PrefixesV4[i].LessThan(res.PrefixesV4[j])
	})
	sort.Slice(res.PrefixesV6, func(i, j int) bool {
		return res.PrefixesV6[i].LessThan(res.PrefixesV6[j])
	})
	return res, nil
}

func filterForSelector(selector v1beta1.PrefixSelector) (frr.IncomingFilter, error) {
	_, cidr, err := net.ParseCIDR(selector.Prefix)
	if err != nil {
		return frr.IncomingFilter{}, fmt.Errorf("failed to parse prefix %s: %w", selector.Prefix, err)
	}
	maskLen, _ := cidr.Mask.Size()
	maskLenUint, err := safeconvert.IntToUInt32(maskLen)
	if err != nil {
		return frr.IncomingFilter{}, fmt.Errorf("failed to convert maskLen from CIDR %s to uint32: %w", cidr, err)
	}
	err = validateSelectorLengths(maskLenUint, selector.LE, selector.GE)
	if err != nil {
		return frr.IncomingFilter{}, err
	}

	family := ipfamily.ForCIDRString(selector.Prefix)

	return frr.IncomingFilter{
		Prefix:   selector.Prefix,
		IPFamily: family,
		GE:       selector.GE,
		LE:       selector.LE,
	}, nil
}

// validateSelectorLengths checks the lengths respect the following
// condition: mask length <= ge <= le
func validateSelectorLengths(mask, le, ge uint32) error {
	if ge == 0 && le == 0 {
		return nil
	}
	if le > 0 && ge > le {
		return fmt.Errorf("invalid selector lengths: ge %d is bigger than le %d", ge, le)
	}
	if le > 0 && mask > le {
		return fmt.Errorf("invalid selector lengths: cidr mask %d is bigger than le %d", mask, le)
	}
	if ge > 0 && mask > ge {
		return fmt.Errorf("invalid selector lengths: cidr mask %d is bigger than ge %d", mask, ge)
	}
	return nil
}

func validateImportVRFs(r v1beta1.Router, allVRFs map[string][]string) error {
	for _, i := range r.Imports {
		if i.VRF == "default" {
			continue
		}
		if _, ok := allVRFs[i.VRF]; !ok {
			return fmt.Errorf("router %d-%s imports vrf %s which is not defined", r.ASN, r.VRF, i.VRF)
		}
	}
	return nil
}

func validateOutgoingPrefixes(prefixesInRouter []string, routerConfig v1beta1.Router) error {
	prefixesSet := sets.New(prefixesInRouter...)
	for _, n := range routerConfig.Neighbors {
		if n.ToAdvertise.Allowed.Mode == v1beta1.AllowAll {
			continue
		}
		for _, p := range n.ToAdvertise.Allowed.Prefixes {
			if !prefixesSet.Has(p) {
				return fmt.Errorf("trying to advertise non configured prefix %s to neighbor %s, vrf %s", p, neighborName(n), routerConfig.VRF)
			}
		}
	}
	return nil
}

func asnFor(n v1beta1.Neighbor) string {
	asn := strconv.FormatUint(uint64(n.ASN), 10)
	if n.DynamicASN != "" {
		asn = string(n.DynamicASN)
	}

	return asn
}

func neighborName(n v1beta1.Neighbor) string {
	if n.Address == "" {
		return fmt.Sprintf("%s@%s", asnFor(n), n.Interface)
	}
	return fmt.Sprintf("%s@%s", asnFor(n), n.Address)
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

	seen := make(map[uint32]bool)
	for _, pfxs := range withLocalPref {
		if _, exists := seen[pfxs.LocalPref]; exists {
			// error when input is withLocalPref = []v1beta1.LocalPrefPrefixes{
			//	                      Prefixes:  []string{"192.0.2.0/24"},LocalPref: 100}, []string{"100.0.0.0/24"},LocalPref: 100}}
			// so we enforce       withLocalPref = []v1beta1.LocalPrefPrefixes{
			//	                      Prefixes:  []string{"192.0.2.0/24, "100.0.0.0/24"},LocalPref: 100}}
			return localPrefPrefixes{}, fmt.Errorf("multiple entries with the same localPref: %d", pfxs.LocalPref)
		}
		seen[pfxs.LocalPref] = true

		for _, p := range pfxs.Prefixes {
			family := ipfamily.ForCIDRString(p)
			lpMap := res.localPrefForPrefixV4
			if family == ipfamily.IPv6 {
				lpMap = res.localPrefForPrefixV6
			}

			v, ok := lpMap[p]
			if !ok {
				lpMap[p] = pfxs.LocalPref
				continue
			}

			if v != pfxs.LocalPref {
				return localPrefPrefixes{}, fmt.Errorf("multiple local prefs (%d,%d) specified for prefix %s", v, pfxs.LocalPref, p)
			}
			if v == pfxs.LocalPref {
				return localPrefPrefixes{}, fmt.Errorf("prefix %s with local prefs %d defined twice", p, v)
			}
		}
	}

	return res, nil
}

func bfdProfileToFRR(bfdProfile v1beta1.BFDProfile) *frr.BFDProfile {
	res := &frr.BFDProfile{
		Name:             bfdProfile.Name,
		ReceiveInterval:  bfdProfile.ReceiveInterval,
		TransmitInterval: bfdProfile.TransmitInterval,
		DetectMultiplier: bfdProfile.DetectMultiplier,
		EchoInterval:     bfdProfile.EchoInterval,
		MinimumTTL:       bfdProfile.MinimumTTL,
	}

	if bfdProfile.EchoMode != nil {
		res.EchoMode = *bfdProfile.EchoMode
	}
	if bfdProfile.PassiveMode != nil {
		res.PassiveMode = *bfdProfile.PassiveMode
	}

	return res
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

func alwaysBlockToFRR(cidrs []net.IPNet) []frr.IncomingFilter {
	res := make([]frr.IncomingFilter, 0, len(cidrs))
	for _, c := range cidrs {
		c := c // to make go sec happy
		filter := frr.IncomingFilter{IPFamily: ipfamily.ForCIDR(&c), Prefix: c.String()}
		filter.LE = uint32(32)
		if filter.IPFamily == ipfamily.IPv6 {
			filter.LE = uint32(128)
		}
		res = append(res, filter)
	}
	return res
}

func parseTimers(ht, ka *v1.Duration) (*int64, *int64, error) {
	if ht == nil && ka != nil || ht != nil && ka == nil {
		return nil, nil, fmt.Errorf("one of KeepaliveTime/HoldTime specified, both must be set or none")
	}

	if ht == nil && ka == nil {
		return nil, nil, nil
	}

	holdTime := ht.Duration
	keepaliveTime := ka.Duration

	rounded := time.Duration(int(ht.Seconds())) * time.Second
	if rounded != 0 && rounded < 3*time.Second {
		return nil, nil, fmt.Errorf("invalid hold time %q: must be 0 or >=3s", ht)
	}

	if keepaliveTime > holdTime {
		return nil, nil, fmt.Errorf("invalid keepaliveTime %q, must be lower than holdTime %q", ka, ht)
	}

	htSeconds := int64(holdTime / time.Second)
	kaSeconds := int64(keepaliveTime / time.Second)

	return &htSeconds, &kaSeconds, nil
}

func validatePrefixes(prefixes []string) error {
	for _, p := range prefixes {
		if ipfamily.ForCIDRString(p) == ipfamily.Unknown {
			return fmt.Errorf("unknown ipfamily for %s", p)
		}
	}
	return nil
}

func prefixesForVRFs(routers []v1beta1.Router) map[string][]string {
	res := map[string][]string{}
	for _, r := range routers {
		res[r.VRF] = r.Prefixes
	}
	return res
}

func importedPrefixes(r v1beta1.Router, prefixesInRouter map[string][]string) ([]string, error) {
	res := []string{}
	for _, i := range r.Imports {
		vrf := i.VRF
		if i.VRF == "default" { // we use default when importing, but leave empty when declaring
			vrf = ""
		}
		imported, ok := prefixesInRouter[vrf]
		if !ok {
			return nil, fmt.Errorf("vrf %s not found in prefixes in router", vrf)
		}
		res = append(res, imported...)
	}
	return res, nil
}

func validateRouterConfig(r *frr.RouterConfig) error {
	// merging with itself to validate neighbor list
	_, err := mergeRouterConfigs(r, r)
	return err
}
