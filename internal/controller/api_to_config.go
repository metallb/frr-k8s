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
	res.Outgoing, err = toAdvertiseToFRR(res, n.ToAdvertise, prefixesInRouter)
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

func toAdvertiseToFRR(neighbor *frr.NeighborConfig, toAdvertise v1beta1.Advertise, prefixesInRouter []string) (frr.AllowedOut, error) {
	neighborIPFamilies := []ipfamily.Family{neighbor.IPFamily}
	if neighbor.IPFamily == ipfamily.DualStack {
		neighborIPFamilies = []ipfamily.Family{ipfamily.IPv4, ipfamily.IPv6}
	}
	prefixesForFamily := map[ipfamily.Family]sets.Set[string]{
		ipfamily.IPv4: prefixesToAdvertiseForFamily(toAdvertise, prefixesInRouter, ipfamily.IPv4),
		ipfamily.IPv6: prefixesToAdvertiseForFamily(toAdvertise, prefixesInRouter, ipfamily.IPv6),
	}

	res := frr.AllowedOut{
		PrefixesV4:                 make([]string, 0),
		PrefixesV6:                 make([]string, 0),
		LocalPrefPrefixesModifiers: make(map[string]frr.LocalPrefPrefixList),
		CommunityPrefixesModifiers: make(map[string]frr.CommunityPrefixList),
	}

	if neighborHasIPFamily(neighbor, ipfamily.IPv4) {
		res.PrefixesV4 = sets.List(prefixesForFamily[ipfamily.IPv4])
	}
	if neighborHasIPFamily(neighbor, ipfamily.IPv6) {
		res.PrefixesV6 = sets.List(prefixesForFamily[ipfamily.IPv6])
	}

	for _, ipFamily := range neighborIPFamilies {
		var err error
		res.LocalPrefPrefixesModifiers, err = prefixesWithLocalPrefToFRR(res.LocalPrefPrefixesModifiers, neighbor, toAdvertise, ipFamily, prefixesForFamily[ipFamily])
		if err != nil {
			return frr.AllowedOut{}, fmt.Errorf("failed to process local pref for neighbor %s, err: %w", neighbor.Name, err)
		}
		res.CommunityPrefixesModifiers, err = prefixesWithCommunityToFRR(res.CommunityPrefixesModifiers, neighbor, toAdvertise, ipFamily, prefixesForFamily[ipFamily])
		if err != nil {
			return frr.AllowedOut{}, fmt.Errorf("failed to process local pref for neighbor %s, err: %w", neighbor.Name, err)
		}
	}

	return res, nil
}

func prefixesWithLocalPrefToFRR(toAdd map[string]frr.LocalPrefPrefixList, neighbor *frr.NeighborConfig, toAdvertise v1beta1.Advertise, ipFamily ipfamily.Family, routerPrefixes sets.Set[string]) (map[string]frr.LocalPrefPrefixList, error) {
	frrFamily := frrIPFamily(ipFamily)
	for _, prefixes := range toAdvertise.PrefixesWithLocalPref {
		key := localPrefPrefixListKey(prefixes.LocalPref, frrFamily)

		if _, ok := toAdd[key]; ok {
			return nil, fmt.Errorf("local preference %d is already defined", prefixes.LocalPref)
		}

		localPrefPrefixList := frr.LocalPrefPrefixList{
			PrefixList: frr.PrefixList{
				Name:     localPrefPrefixListName(neighbor.ID(), prefixes.LocalPref, frrFamily),
				IPFamily: frrFamily,
				Prefixes: sets.New[string](),
			},
			LocalPref: prefixes.LocalPref,
		}

		ipfamilyPrefixes := ipfamily.FilterPrefixes(prefixes.Prefixes, ipFamily)
		for _, prefix := range ipfamilyPrefixes {
			if !routerPrefixes.Has(prefix) {
				return nil, fmt.Errorf("localPref %d associated to non existing prefix %s", prefixes.LocalPref, prefix)
			}
			if localPrefPrefixList.Prefixes.Has(prefix) {
				return nil, fmt.Errorf("prefix %s is already defined for local preference %d", prefix, prefixes.LocalPref)
			}
			if existing, ok := toAdd[prefix]; ok && existing.LocalPref != prefixes.LocalPref {
				return nil, fmt.Errorf("prefix %s is advertised with different local preference %d and %d", prefix, existing.LocalPref, prefixes.LocalPref)
			}

			localPrefPrefixList.Prefixes.Insert(prefix)
		}
		toAdd[key] = localPrefPrefixList
	}
	return toAdd, nil
}

func prefixesWithCommunityToFRR(toAdd map[string]frr.CommunityPrefixList, neighbor *frr.NeighborConfig, toAdvertise v1beta1.Advertise, ipFamily ipfamily.Family, routerPrefixes sets.Set[string]) (map[string]frr.CommunityPrefixList, error) {
	for _, prefixes := range toAdvertise.PrefixesWithCommunity {
		c, err := community.New(prefixes.Community)
		if err != nil {
			return nil, fmt.Errorf("invalid community %s, err: %w", prefixes.Community, err)
		}
		frrFamily := frrIPFamily(ipFamily)

		key := communityPrefixListKey(c, frrFamily)
		if _, ok := toAdd[key]; ok {
			return nil, fmt.Errorf("community %s is already defined", prefixes.Community)
		}

		communityPrefixList := frr.CommunityPrefixList{
			PrefixList: frr.PrefixList{
				Name:     communityPrefixListName(neighbor.ID(), c, frrFamily),
				IPFamily: frrFamily,
				Prefixes: sets.New[string](),
			},
			Community: c,
		}

		ipfamilyPrefixes := ipfamily.FilterPrefixes(prefixes.Prefixes, ipFamily)
		for _, prefix := range ipfamilyPrefixes {
			if !routerPrefixes.Has(prefix) {
				return nil, fmt.Errorf("prefix %s is advertised for community %s but it's not in the advertisement list of the neighbor", prefix, c)
			}
			if communityPrefixList.Prefixes.Has(prefix) {
				return nil, fmt.Errorf("prefix %s is already defined for community %s", prefix, c)
			}
			communityPrefixList.Prefixes.Insert(prefix)
		}
		toAdd[key] = communityPrefixList
	}
	return toAdd, nil
}

func neighborHasIPFamily(neighbor *frr.NeighborConfig, ipFamily ipfamily.Family) bool {
	if neighbor.IPFamily == ipfamily.DualStack {
		return true
	}
	return neighbor.IPFamily == ipFamily
}

func prefixesToAdvertiseForFamily(toAdvertise v1beta1.Advertise, prefixesInRouter []string, ipFamily ipfamily.Family) sets.Set[string] {
	res := sets.New[string]()
	prefixesForFamily := ipfamily.FilterPrefixes(prefixesInRouter, ipFamily)
	if toAdvertise.Allowed.Mode == v1beta1.AllowAll {
		for _, p := range prefixesForFamily {
			res.Insert(p)
		}
		return res
	}

	for _, p := range toAdvertise.Allowed.Prefixes {
		prefixFamily := ipfamily.ForCIDRString(p)
		if prefixFamily == ipFamily {
			res.Insert(p)
		}
	}
	return res
}

func frrIPFamily(ipFamily ipfamily.Family) string {
	if ipFamily == "ipv6" {
		return "ipv6"
	}
	return "ip"
}

func localPrefPrefixListName(neighborID string, localPreference uint32, ipFamily string) string {
	return fmt.Sprintf("%s-%d-%s-localpref-prefixes", neighborID, localPreference, ipFamily)
}

func communityPrefixListName(neighborID string, comm community.BGPCommunity, ipFamily string) string {
	if community.IsLarge(comm) {
		return fmt.Sprintf("%s-large:%s-%s-community-prefixes", neighborID, comm, ipFamily)
	}
	return fmt.Sprintf("%s-%s-%s-community-prefixes", neighborID, comm, ipFamily)
}

func communityPrefixListKey(comm community.BGPCommunity, ipFamily string) string {
	return fmt.Sprintf("%s-%s", comm, ipFamily)
}

func localPrefPrefixListKey(localPref uint32, ipFamily string) string {
	return fmt.Sprintf("%d-%s", localPref, ipFamily)
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
		localPrefForPrefix := map[string]uint32{}
		for _, prefixes := range n.ToAdvertise.PrefixesWithLocalPref {
			for _, p := range prefixes.Prefixes {
				if ipfamily.ForCIDRString(p) == ipfamily.Unknown {
					return fmt.Errorf("unknown ipfamily for prefix %s associated to localpref %d", p, prefixes.LocalPref)
				}
				if existing, ok := localPrefForPrefix[p]; ok && existing != prefixes.LocalPref {
					return fmt.Errorf("prefix %s is configured with both local preference %d and %d", prefixes.Prefixes, existing, prefixes.LocalPref)
				}
				localPrefForPrefix[p] = prefixes.LocalPref
			}
		}

		for _, prefixes := range n.ToAdvertise.PrefixesWithCommunity {
			if err := validatePrefixes(prefixes.Prefixes); err != nil {
				return fmt.Errorf("invalid prefixes %s for community %s, err: %w", prefixes.Prefixes, prefixes.Community, err)
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
