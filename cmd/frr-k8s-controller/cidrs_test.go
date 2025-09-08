// SPDX-License-Identifier:Apache-2.0

package main

import (
	"net"
	"testing"
)

func TestParseCIDRS(t *testing.T) {
	_, ipv4CIDR, _ := net.ParseCIDR("192.168.1.2/24")
	_, ipv6CIDR, _ := net.ParseCIDR("fc00:f853:ccd:e800::/64")
	tests := []struct {
		name     string
		cidrs    string
		expected []net.IPNet
		mustFail bool
	}{
		{
			name:     "empty",
			cidrs:    "",
			expected: nil,
		},
		{
			name:     "simple",
			cidrs:    "192.168.1.2/24,fc00:f853:ccd:e800::/64",
			expected: []net.IPNet{*ipv4CIDR, *ipv6CIDR},
		},
		{
			name:     "with spaces",
			cidrs:    "192.168.1.2/24 , fc00:f853:ccd:e800::/64 ",
			expected: []net.IPNet{*ipv4CIDR, *ipv6CIDR},
		},
		{
			name:     "invalid cidr",
			cidrs:    "192.168.1.2.1/24 , fc00:f853:ccd:e800::/64",
			mustFail: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := parseCIDRs(test.cidrs)
			if !test.mustFail && err != nil {
				t.Fatalf("error was not expected but got %v", err)
			}
			if test.mustFail && err == nil {
				t.Fatal("error was expected")
			}
			if len(res) != len(test.expected) {
				t.Fatalf("Len is different: res [%v] expected [%v]", res, test.expected)
			}
			for i := range res {
				if res[i].String() != test.expected[i].String() {
					t.Fatalf("Element %d is different: res %s expected %s", i, res[i], test.expected[i])
				}
			}
		})
	}
}
