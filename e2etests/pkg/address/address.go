// SPDX-License-Identifier:Apache-2.0

package address

import (
	"net"

	"go.universe.tf/e2etest/pkg/ipfamily"
)

func FilterForFamily(ips []string, family ipfamily.Family) []string {
	if family == ipfamily.DualStack {
		return ips
	}
	var res []string
	for _, ip := range ips {
		parsedIP, _, _ := net.ParseCIDR(ip)
		if parsedIP == nil {
			panic("invalid ip") // it's a test after all, should never happen
		}
		isV4 := (parsedIP.To4() != nil)
		if family == ipfamily.IPv4 && isV4 {
			res = append(res, ip)
			continue
		}
		if family == ipfamily.IPv6 && !isV4 {
			res = append(res, ip)
			continue
		}
	}
	return res
}
