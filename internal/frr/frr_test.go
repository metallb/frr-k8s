// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/metallb/frr-k8s/internal/community"
	"github.com/metallb/frr-k8s/internal/ipfamily"
	"github.com/metallb/frr-k8s/internal/logging"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
)

const testData = "testdata/"

var update = flag.Bool("update", false, "update .golden files")

func testOsHostname() (string, error) {
	return "dummyhostname", nil
}

func testCompareFiles(t *testing.T, configFile, goldenFile string) {
	var lastError error

	// Try comparing files multiple times because tests can generate more than one configuration
	err := wait.PollUntilContextTimeout(context.TODO(), 10*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		lastError = nil
		cmd := exec.Command("diff", configFile, goldenFile)
		output, err := cmd.Output()

		if err != nil {
			lastError = fmt.Errorf("command %s returned error: %s\n%s", cmd.String(), err, output)
			return false, nil
		}

		return true, nil
	})

	// err can only be a ErrWaitTimeout, as the check function always return nil errors.
	// So lastError is always set
	if err != nil {
		t.Fatalf("failed to compare configfiles %s, %s using poll interval\nlast error: %v", configFile, goldenFile, lastError)
	}
}

func testUpdateGoldenFile(t *testing.T, configFile, goldenFile string) {
	t.Log("update golden file")

	// Sleep to be sure the sessionManager has produced all configuration the test
	// has triggered and no config is still waiting in the debouncer() local variables.
	// No other conditions can be checked, so sleeping is our best option.
	time.Sleep(100 * time.Millisecond)

	cmd := exec.Command("cp", "-a", configFile, goldenFile)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %s returned %s and error: %s", cmd.String(), output, err)
	}
}

func testGenerateFileNames(t *testing.T) (string, string) {
	return filepath.Join(testData, filepath.FromSlash(t.Name())), filepath.Join(testData, filepath.FromSlash(t.Name())+".golden")
}

func testSetup(t *testing.T) {
	configFile, _ := testGenerateFileNames(t)
	os.Setenv("FRR_CONFIG_FILE", configFile)
	_ = os.Remove(configFile) // removing leftovers from previous runs
	osHostname = testOsHostname
}

func testCheckConfigFile(t *testing.T) {
	configFile, goldenFile := testGenerateFileNames(t)

	if *update {
		testUpdateGoldenFile(t, configFile, goldenFile)
	}

	testCompareFiles(t, configFile, goldenFile)

	if !strings.Contains(configFile, "Invalid") {
		err := testFileIsValid(configFile)
		if err != nil {
			t.Fatalf("Failed to verify the file %s", err)
		}
	}
}

var emptyCB = func() {}

func TestSingleSession(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Port:     ptr.To[uint16](4567),
						Outgoing: AllowedOut{
							PrefixesV4: []string{
								"192.169.1.0/24",
								"192.170.1.0/22",
							},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestTwoRoutersTwoNeighbors(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:      ipfamily.IPv4,
						ASN:           "65001",
						Addr:          "192.168.1.2",
						HoldTime:      ptr.To[int64](80),
						KeepaliveTime: ptr.To[int64](40),
						ConnectTime:   ptr.To(int64(10)),
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.169.1.0/22", "192.170.1.0/22"},
							PrefixesV6: []string{},
							CommunityPrefixesModifiers: []CommunityPrefixList{
								communityPrefixListFor("65001@192.168.1.2", "10:169", "ip", "192.0.2.0/24"),
								communityPrefixListFor("65001@192.168.1.2", "10:170", "ip", "192.0.2.0/24", "192.169.1.0/22", "192.170.1.0/22"),
								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7892", "ip", "192.0.2.0/24"),
								communityPrefixListFor("65040@192.0.1.23", "20:200", "ipv6", "2001:db8::/64"),
								communityPrefixListFor("65040@192.0.1.23", "large:123:456:7890", "ipv6", "2001:db8::/64"),
							},
							LocalPrefPrefixesModifiers: []LocalPrefPrefixList{
								localPrefPrefixListFor("65001@192.168.1.2", 100, "ip", "192.0.2.0/24"),
								localPrefPrefixListFor("65001@192.168.1.2", 150, "ip", "192.169.1.0/22"),
							},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
			{
				MyASN: 65000,
				VRF:   "red",
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.3",
						Outgoing: AllowedOut{
							PrefixesV4:                 []string{"192.169.1.0/24"},
							PrefixesV6:                 []string{},
							CommunityPrefixesModifiers: []CommunityPrefixList{},
							LocalPrefPrefixesModifiers: []LocalPrefPrefixList{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestTwoSessionsAcceptAll(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Incoming: AllowedIn{
							All: true,
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.3",
						Incoming: AllowedIn{
							All: true,
						},
					},
				},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestTwoSessionsAcceptSomeV4(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.1.0/24",
								},
							},
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.3",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.170.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
							},
						},
					},
				},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestTwoSessionsAcceptV4AndV6(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e800::/64",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.2.0/24",
									LE:       32,
									GE:       24,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.3.0/24",
									LE:       32,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.3.0/24",
									GE:       16,
								},
							},
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      "65002",
						Addr:     "192.168.1.3",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e800::/64",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.2.0/24",
									LE:       26,
									GE:       24,
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.3.0/24",
									LE:       32,
									GE:       27,
								},
							},
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.4",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e799::/64",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e800::/64",
									LE:       32,
									GE:       24,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e801::/64",
									GE:       24,
								},
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e802::/64",
									LE:       32,
								},
							},
						},
					},
				},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithEBGPMultihop(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:     ipfamily.IPv4,
						ASN:          "65001",
						Addr:         "192.168.1.2",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.170.1.0/22"},
							PrefixesV6: []string{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithIPv6SingleHop(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:     ipfamily.IPv6,
						ASN:          "65001",
						Addr:         "2001:db8::1",
						EBGPMultiHop: false, // Single hop
						Outgoing: AllowedOut{
							PrefixesV4: []string{},
							PrefixesV6: []string{"2001:db8:abcd::/48"},
						},
					},
				},
				IPV6Prefixes: []string{"2001:db8:abcd::/48"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestMultipleNeighborsOneV4AndOneV6(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						SrcAddr:  "192.168.1.50",
						Addr:     "192.168.1.2",
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24"},
							PrefixesV6: []string{},
						},
					},
					{
						IPFamily:     ipfamily.IPv6,
						ASN:          "65002",
						Addr:         "2001:db8::1",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{},
							PrefixesV6: []string{"2001:db8:abcd::/48"},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24"},
				IPV6Prefixes: []string{"2001:db8:abcd::/48"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestMultipleNeighborsOneV4AndOneV6DualStackIPFamily(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.DualStack,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24"},
							PrefixesV6: []string{},
						},
					},
					{
						IPFamily:     ipfamily.DualStack,
						ASN:          "65002",
						Addr:         "2001:db8::1",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{},
							PrefixesV6: []string{"2001:db8:abcd::/48"},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24"},
				IPV6Prefixes: []string{"2001:db8:abcd::/48"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestMultipleRoutersMultipleNeighs(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:     ipfamily.IPv4,
						ASN:          "65001",
						Addr:         "192.168.1.2",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24"},
							PrefixesV6: []string{},
						},
					},
					{
						IPFamily:     ipfamily.IPv6,
						ASN:          "65002",
						Addr:         "2001:db8::1",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{},
							PrefixesV6: []string{"2001:db8:abcd::/48"},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24"},
				IPV6Prefixes: []string{"2001:db8:abcd::/48"},
			},
			{
				MyASN: 65000,
				VRF:   "red",
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.170.1.2",
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.171.1.0/24"},
							PrefixesV6: []string{},
						},
					},
					{
						IPFamily:     ipfamily.IPv6,
						ASN:          "65002",
						Addr:         "2001:db9::1",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{},
							PrefixesV6: []string{"2001:db8:abcd::/48"},
						},
					},
				},
				IPV4Prefixes: []string{"192.171.1.0/24"},
				IPV6Prefixes: []string{"2001:db9:abcd::/48"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithEBGPMultihopAndExtras(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:     ipfamily.IPv4,
						ASN:          "65001",
						Addr:         "192.168.1.2",
						EBGPMultiHop: true,
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.170.1.0/22"},
							PrefixesV6: []string{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel:    LevelFrom(logging.LevelInfo),
		ExtraConfig: "# foo\n# baar",
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithAlwaysBlock(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.2",
						Incoming: AllowedIn{
							All: true,
						},
						AlwaysBlock: []IncomingFilter{
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.168.1.0/24",
								LE:       uint32(24),
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "fc00:f853:ccd:e800::/64",
								LE:       uint32(64),
							},
						},
					},
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "65001",
						Addr:     "192.168.1.6",
						Incoming: AllowedIn{
							PrefixesV4: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.2.0/24",
								},
							},
						},
						AlwaysBlock: []IncomingFilter{
							{
								IPFamily: ipfamily.IPv4,
								Prefix:   "192.168.1.0/24",
								LE:       uint32(24),
							},
							{
								IPFamily: ipfamily.IPv6,
								Prefix:   "fc00:f853:ccd:e800::/64",
								LE:       uint32(64),
							},
						},
					},
				},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithGracefulRestart(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:        ipfamily.IPv4,
						ASN:             "65001",
						Addr:            "192.168.1.2",
						GracefulRestart: true,
					},
				},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestMultipleRoutersImportVRFs(t *testing.T) {
	testSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frr := NewFRR(ctx, emptyCB)

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily:     ipfamily.IPv4,
						ASN:          "65001",
						Addr:         "192.168.1.2",
						EBGPMultiHop: true,
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24"},
				IPV6Prefixes: []string{"2001:db8:abcd::/48"},
				ImportVRFs:   []string{"red"},
			},
			{
				MyASN:        65000,
				VRF:          "red",
				IPV4Prefixes: []string{"192.171.1.0/24"},
			},
			{
				MyASN:        65000,
				VRF:          "blue",
				IPV4Prefixes: []string{"192.171.1.0/24"},
				IPV6Prefixes: []string{"2001:db9:abcd::/48"},
				ImportVRFs:   []string{"default"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}

	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithInternalASN(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "internal",
						Addr:     "192.168.1.2",
						Port:     ptr.To[uint16](4567),
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.170.1.0/22"},
							PrefixesV6: []string{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSingleSessionWithExternalASN(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "external",
						Addr:     "192.168.1.2",
						Port:     ptr.To[uint16](4567),
						Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.170.1.0/22"},
							PrefixesV6: []string{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}
func TestSingleUnnumberedSession(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Routers: []*RouterConfig{
			{
				MyASN: 65000,
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      "external",
						Addr:     "",
						Iface:    "net0",
						Port:     ptr.To[uint16](4567), Outgoing: AllowedOut{
							PrefixesV4: []string{"192.169.1.0/24", "192.170.1.0/22"},
							PrefixesV6: []string{},
						},
					},
				},
				IPV4Prefixes: []string{"192.169.1.0/24", "192.170.1.0/22"},
			},
		},
		Loglevel: LevelFrom(logging.LevelInfo),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

// TestLogLevelDebugging validates that when the log level is set to debug, the FRR configuration
// includes both the `log stdout debugging` directive and all associated debug statements for
// zebra, bgp, and bfd subsystems. This test is necessary because the other tests use info-level
// logging and do not verify that debug-specific configuration is properly generated. It also
// checks that *logging.LevelFRR is correctly rendered in the templates.
func TestLogLevelDebugging(t *testing.T) {
	testSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	frr := NewFRR(ctx, emptyCB)
	defer cancel()

	config := Config{
		Loglevel: LevelFrom(logging.LevelDebug),
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func communityPrefixListFor(neigID, comm string, ipFamily string, prefixes ...string) CommunityPrefixList {
	community, err := community.New(comm)
	if err != nil {
		panic(err)
	}
	return CommunityPrefixList{
		PrefixList: PrefixList{
			Name:     communityPrefixListName(neigID, community, ipFamily),
			Prefixes: sets.New(prefixes...),
			IPFamily: ipFamily,
		},
		Community: community,
	}
}

func localPrefPrefixListFor(neigID string, localPref int, ipFamily string, prefixes ...string) LocalPrefPrefixList {
	return LocalPrefPrefixList{
		PrefixList: PrefixList{
			Name:     localPrefPrefixListName(neigID, uint32(localPref), ipFamily),
			Prefixes: sets.New(prefixes...),
			IPFamily: ipFamily,
		},
		LocalPref: uint32(localPref),
	}
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
