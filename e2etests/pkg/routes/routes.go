// SPDX-License-Identifier:Apache-2.0

package routes

import (
	"bytes"
	"fmt"
	"net"

	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/executor"
	"go.universe.tf/e2etest/pkg/frr"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	v1 "k8s.io/api/core/v1"
)

// PodHasPrefixFromContainer tells if the given frr-k8s pod has recevied a route for
// the given prefix from the given container.
func PodHasPrefixFromContainer(pod *v1.Pod, frr frrcontainer.FRR, vrf, prefix string) bool {
	_, cidr, _ := net.ParseCIDR(prefix)
	ipFamily := ipfamily.ForCIDR(cidr)
	nextHop := frr.Ipv4
	if ipFamily == ipfamily.IPv6 {
		nextHop = frr.Ipv6
	}
	return hasPrefix(pod, ipFamily, cidr, nextHop, vrf)
}

// CheckNeighborHasPrefix tells if the given frr container has a route toward the given prefix
// via the set of node passed to this function.
func CheckNeighborHasPrefix(neighbor frrcontainer.FRR, vrf, prefix string, nodes []v1.Node) error {
	routesV4, routesV6, err := frr.Routes(neighbor)
	if err != nil {
		return fmt.Errorf("Routes %w", err)
	}

	_, cidr, err := net.ParseCIDR(prefix)
	if err != nil {
		return fmt.Errorf("ParseCIDR %w", err)
	}

	route, err := routeForCIDR(cidr, routesV4, routesV6)
	if err != nil {
		return fmt.Errorf("routerForCIDR: %w", err)
	}

	cidrFamily := ipfamily.ForCIDR(cidr)
	err = frr.RoutesMatchNodes(nodes, route, cidrFamily, vrf)
	if err != nil {
		return fmt.Errorf("RoutesMatchNodes %w", err)
	}
	return nil
}

func cidrsAreEqual(a, b *net.IPNet) bool {
	return a.IP.Equal(b.IP) && bytes.Equal(a.Mask, b.Mask)
}

type RouteNotFoundError struct {
	Route string
}

func (e RouteNotFoundError) Error() string {
	return fmt.Sprintf("route not found for dst prefix %s", e.Route)
}

func routeForCIDR(cidr *net.IPNet, routesV4 map[string]frr.Route, routesV6 map[string]frr.Route) (frr.Route, error) {
	for _, route := range routesV4 {
		if cidrsAreEqual(route.Destination, cidr) {
			return route, nil
		}
	}
	for _, route := range routesV6 {
		if cidrsAreEqual(route.Destination, cidr) {
			return route, nil
		}
	}
	return frr.Route{}, RouteNotFoundError{Route: cidr.String()}
}

func hasPrefix(pod *v1.Pod, pairingFamily ipfamily.Family, prefix *net.IPNet, nextHop, vrf string) bool {
	found := false
	podExec := executor.ForPod(pod.Namespace, pod.Name, "frr")
	routes, frrRoutesV6, err := frr.RoutesForVRF(vrf, podExec)
	Expect(err).NotTo(HaveOccurred())

	if pairingFamily == ipfamily.IPv6 {
		routes = frrRoutesV6
	}

out:
	for _, route := range routes {
		if !cidrsAreEqual(route.Destination, prefix) {
			continue
		}
		for _, nh := range route.NextHops {
			if nh.String() == nextHop {
				found = true
				break out
			}
		}
	}
	return found
}
