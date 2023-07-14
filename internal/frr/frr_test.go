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

	"github.com/go-kit/log"
	"github.com/metallb/frrk8s/internal/ipfamily"
	"github.com/metallb/frrk8s/internal/logging"
	"k8s.io/apimachinery/pkg/util/wait"
)

const testData = "testdata/"

var update = flag.Bool("update", false, "update .golden files")

func testOsHostname() (string, error) {
	return "dummyhostname", nil
}

func testCompareFiles(t *testing.T, configFile, goldenFile string) {
	var lastError error

	// Try comparing files multiple times because tests can generate more than one configuration
	err := wait.PollImmediate(10*time.Millisecond, 2*time.Second, func() (bool, error) {
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

func TestSingleSession(t *testing.T) {
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
						Outgoing: AllowedOut{
							Prefixes: []OutgoingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.170.1.0/22",
								},
							},
						},
					},
				},
			},
		},
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
						Outgoing: AllowedOut{
							Prefixes: []OutgoingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.170.1.0/22",
								},
							},
						},
					},
				},
			},
			{
				MyASN:        65000,
				VRF:          "red",
				IPV4Prefixes: []string{"192.169.1.0/24"},
				Neighbors: []*NeighborConfig{
					{
						IPFamily: ipfamily.IPv4,
						ASN:      65001,
						Addr:     "192.168.1.3",
						Outgoing: AllowedOut{
							Prefixes: []OutgoingFilter{
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
						Incoming: AllowedIn{
							All: true,
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      65001,
						Addr:     "192.168.1.3",
						Port:     4567,
						Incoming: AllowedIn{
							All: true,
						},
					},
				},
			},
		},
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
						Incoming: AllowedIn{
							Prefixes: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.1.0/24",
								},
							},
							HasV4: true,
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      65001,
						Addr:     "192.168.1.3",
						Port:     4567,
						Incoming: AllowedIn{
							Prefixes: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.170.1.0/24",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
							},
							HasV4: true,
						},
					},
				},
			},
		},
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
						Incoming: AllowedIn{
							Prefixes: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e800::/64",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.168.1.0/24",
								},
							},
							HasV4: true,
							HasV6: true,
						},
					}, {
						IPFamily: ipfamily.IPv4,
						ASN:      65001,
						Addr:     "192.168.1.3",
						Port:     4567,
						Incoming: AllowedIn{
							Prefixes: []IncomingFilter{
								{
									IPFamily: ipfamily.IPv6,
									Prefix:   "fc00:f853:ccd:e799::/64",
								},
								{
									IPFamily: ipfamily.IPv4,
									Prefix:   "192.169.1.0/24",
								},
							},
							HasV4: true,
							HasV6: true,
						},
					},
				},
			},
		},
	}
	err := frr.ApplyConfig(&config)
	if err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}
