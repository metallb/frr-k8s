// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.universe.tf/e2etest/pkg/executor"
	metallbfrr "go.universe.tf/e2etest/pkg/frr"
)

// NeighborHasEVPN checks that the given neighbor has the l2vpnEvpn address
// family active and is connected.
func HasEVPNNeighbor(exec executor.Executor, neighborIP string) error {
	neighbors, err := metallbfrr.NeighborsInfo(exec)
	if err != nil {
		return err
	}
	for _, n := range neighbors {
		if n.ID == neighborIP || n.BGPNeighborAddr == neighborIP {
			for _, af := range n.AddressFamilies {
				if strings.Contains(strings.ToLower(af), "l2vpnevpn") {
					if !n.Connected {
						return fmt.Errorf("neighbor %s has l2vpnEvpn but is not connected", neighborIP)
					}
					return nil
				}
			}
			return fmt.Errorf("neighbor %s does not have l2vpnEvpn address family, has: %v", neighborIP, n.AddressFamilies)
		}
	}
	return fmt.Errorf("neighbor %s not found", neighborIP)
}

// VNIExists checks that the given VNI is visible in FRR's EVPN state.
func HasEVPNVNI(exec executor.Executor, vni uint32) error {
	out, err := exec.Exec("vtysh", "-c", fmt.Sprintf("show evpn vni %d json", vni))
	if err != nil {
		return fmt.Errorf("failed to query VNI %d: %w", vni, err)
	}
	var result struct {
		VNI uint32 `json:"vni"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return fmt.Errorf("failed to parse VNI %d output: %w", vni, err)
	}
	if result.VNI != vni {
		return fmt.Errorf("VNI %d not found in EVPN state, got %d", vni, result.VNI)
	}
	return nil
}

// CheckType2Routes verifies that EVPN type-2 (MAC/IP) routes exist for
// all the given MAC addresses.
func HasEVPNType2Routes(exec executor.Executor, expectedMACs []string) error {
	out, err := exec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type macip json")
	if err != nil {
		return fmt.Errorf("failed to query type-2 routes: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return fmt.Errorf("failed to parse type-2 routes: %w", err)
	}

	var routeKeys []string
	for key, val := range top {
		if key == "numPrefix" || key == "numPaths" {
			continue
		}
		var rd map[string]json.RawMessage
		if err := json.Unmarshal(val, &rd); err != nil {
			continue
		}
		for routeKey := range rd {
			routeKeys = append(routeKeys, routeKey)
		}
	}

	var missing []string
	for _, mac := range expectedMACs {
		found := false
		for _, rk := range routeKeys {
			if strings.Contains(strings.ToLower(rk), strings.ToLower(mac)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, mac)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("type-2 routes missing for MACs: %v", missing)
	}
	return nil
}

// CheckType5Routes verifies that EVPN type-5 (prefix) routes exist with
// the expected next-hops.
func HasEVPNType5Routes(exec executor.Executor, expectedRoutes map[string]string) error {
	out, err := exec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type prefix json")
	if err != nil {
		return fmt.Errorf("failed to query type-5 routes: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return fmt.Errorf("failed to parse type-5 routes: %w", err)
	}

	type nexthop struct {
		IP string `json:"ip"`
	}
	type path struct {
		NextHops []nexthop `json:"nexthops"`
	}
	type routeEntry struct {
		Paths [][]path `json:"paths"`
	}

	routeNextHops := map[string][]string{}
	for key, val := range top {
		if key == "numPrefix" || key == "numPaths" {
			continue
		}
		var rd map[string]json.RawMessage
		if err := json.Unmarshal(val, &rd); err != nil {
			continue
		}
		for routeKey, routeVal := range rd {
			if routeKey == "rd" {
				continue
			}
			var entry routeEntry
			if err := json.Unmarshal(routeVal, &entry); err != nil {
				continue
			}
			for _, pathGroup := range entry.Paths {
				for _, p := range pathGroup {
					for _, nh := range p.NextHops {
						routeNextHops[routeKey] = append(routeNextHops[routeKey], nh.IP)
					}
				}
			}
		}
	}

	var missing []string
	for routeKey, expectedNH := range expectedRoutes {
		nhs, ok := routeNextHops[routeKey]
		if !ok {
			missing = append(missing, fmt.Sprintf("%s (not found)", routeKey))
			continue
		}
		found := false
		for _, nh := range nhs {
			if nh == expectedNH {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s (expected NH %s, got %v)", routeKey, expectedNH, nhs))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("type-5 routes missing or wrong: %v", missing)
	}
	return nil
}
