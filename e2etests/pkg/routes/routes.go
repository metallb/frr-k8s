// SPDX-License-Identifier:Apache-2.0

package routes

import (
	"bytes"
	"errors"
	"fmt"
	"net"

	"go.universe.tf/e2etest/pkg/frr"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	v1 "k8s.io/api/core/v1"
)

// CheckNeighborHasPrefix tells if the given frr container has a route toward the given prefix
// via the set of node passed to this function.
func CheckNeighborHasPrefix(neighbor frrcontainer.FRR, prefix string, nodes []v1.Node) (bool, error) {
	routesV4, routesV6, err := frr.Routes(neighbor)
	if err != nil {
		return false, err
	}

	_, cidr, err := net.ParseCIDR(prefix)
	if err != nil {
		return false, err
	}

	route, err := routeForCIDR(cidr, routesV4, routesV6)
	var notFound RouteNotFoundError
	if errors.As(err, &notFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	cidrFamily := ipfamily.ForCIDR(cidr)
	err = frr.RoutesMatchNodes(nodes, route, cidrFamily, neighbor.RouterConfig.VRF)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func cidrsAreEqual(a, b *net.IPNet) bool {
	return a.IP.Equal(b.IP) && bytes.Equal(a.Mask, b.Mask)
}

type RouteNotFoundError string

func (e RouteNotFoundError) Error() string {
	return string(e)
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
	return frr.Route{}, RouteNotFoundError(fmt.Sprintf("route %s not found", cidr))
}
