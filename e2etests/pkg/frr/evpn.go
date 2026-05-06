// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"
	"slices"
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

// HasEVPNType2Routes verifies that EVPN type-2 (MAC/IP) routes exist for
// the expected MACs with the correct next-hops. Keys are MAC addresses,
// values are expected next-hop IPs.
func HasEVPNType2Routes(exec executor.Executor, expectedRoutes map[string]string) error {
	remapped := make(map[string]string, len(expectedRoutes))
	for mac, nh := range expectedRoutes {
		remapped[fmt.Sprintf("[2]:[0]:[48]:[%s]", mac)] = nh
	}
	return hasEVPNRoutes(exec, "show bgp l2vpn evpn route type macip json", remapped)
}

// HasEVPNType5Routes verifies that EVPN type-5 (prefix) routes exist with
// the expected next-hops. Keys are CIDR prefixes (e.g. "10.200.1.0/24"),
// values are expected next-hop IPs.
func HasEVPNType5Routes(exec executor.Executor, expectedRoutes map[string]string) error {
	remapped := make(map[string]string, len(expectedRoutes))
	for cidr, nh := range expectedRoutes {
		parts := strings.SplitN(cidr, "/", 2)
		remapped[fmt.Sprintf("[5]:[0]:[%s]:[%s]", parts[1], parts[0])] = nh
	}
	return hasEVPNRoutes(exec, "show bgp l2vpn evpn route type prefix json", remapped)
}

func hasEVPNRoutes(exec executor.Executor, vtyshCmd string, expectedRoutes map[string]string) error {
	out, err := exec.Exec("vtysh", "-c", vtyshCmd)
	if err != nil {
		return fmt.Errorf("failed to query routes: %w", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		return fmt.Errorf("failed to parse routes: %w", err)
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
	for expected, expectedNH := range expectedRoutes {
		found := false
		for routeKey, nhs := range routeNextHops {
			if strings.HasPrefix(routeKey, expected) {
				if slices.Contains(nhs, expectedNH) {
					found = true
					break
				}
				missing = append(missing, fmt.Sprintf("%s (expected NH %s, got %v)", expected, expectedNH, nhs))
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s (not found)", expected))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("routes missing or wrong: %v", missing)
	}
	return nil
}
