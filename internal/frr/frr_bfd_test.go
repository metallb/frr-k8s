// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/metallb/frrk8s/internal/ipfamily"
	"github.com/metallb/frrk8s/internal/logging"
	"k8s.io/utils/pointer"
)

func TestSingleSessionBFD(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, log.NewNopLogger(), logging.LevelInfo)
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
						Port:       4567,
						BFDProfile: "test",
					},
				},
			},
		},
		BFDProfiles: []BFDProfile{
			{
				Name:             "test",
				ReceiveInterval:  pointer.Uint32(100),
				TransmitInterval: pointer.Uint32(200),
				DetectMultiplier: pointer.Uint32(3),
				EchoInterval:     pointer.Uint32(25),
				EchoMode:         true,
				PassiveMode:      true,
				MinimumTTL:       pointer.Uint32(20),
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
	frr := NewFRR(ctx, log.NewNopLogger(), logging.LevelInfo)
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
						Port:     4567,
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
				ReceiveInterval:  pointer.Uint32(100),
				TransmitInterval: pointer.Uint32(200),
				DetectMultiplier: pointer.Uint32(3),
				EchoInterval:     pointer.Uint32(25),
				EchoMode:         true,
				PassiveMode:      true,
				MinimumTTL:       pointer.Uint32(20),
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
