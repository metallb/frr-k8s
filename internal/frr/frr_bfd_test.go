// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"github.com/metallb/frr-k8s/internal/logging"
	"k8s.io/utils/ptr"
)

func TestSingleSessionBFD(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, func() {}, log.NewNopLogger(), logging.LevelInfo)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:   ipfamily.IPv4,
						ASN:        65001,
						Addr:       "192.168.1.2",
						BFDProfile: "test",
					},
				},
			},
		},
		BFDProfiles: []BFDProfile{
			{
				Name:             "test",
				ReceiveInterval:  ptr.To[uint32](100),
				TransmitInterval: ptr.To[uint32](200),
				DetectMultiplier: ptr.To[uint32](3),
				EchoInterval:     ptr.To[uint32](25),
				EchoMode:         true,
				PassiveMode:      true,
				MinimumTTL:       ptr.To[uint32](20),
			}, {
				Name: "testdefault",
			},
		},
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestTwoRoutersTwoNeighborsBFD(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, func() {}, log.NewNopLogger(), logging.LevelInfo)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      65001,
						Addr:     "192.168.1.2",
					},
				},
			},
			{
				MyASN: 65000,
				VRF:   "red",
				Neighbors: []*NeighborConfig{
					{
						IPFamily:   ipfamily.IPv4,
						ASN:        65001,
						Addr:       "192.168.1.3",
						BFDProfile: "testdefault",
					},
				},
			},
		},
		BFDProfiles: []BFDProfile{
			{
				Name:             "test",
				ReceiveInterval:  ptr.To[uint32](100),
				TransmitInterval: ptr.To[uint32](200),
				DetectMultiplier: ptr.To[uint32](3),
				EchoInterval:     ptr.To[uint32](25),
				EchoMode:         true,
				PassiveMode:      true,
				MinimumTTL:       ptr.To[uint32](20),
			}, {
				Name: "testdefault",
			},
		},
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}
