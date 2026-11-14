// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/metallb/frr-k8s/cmd/metrics/vtysh"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestScrapeFailuresAndRecovery(t *testing.T) {
	for _, tt := range []struct {
		name, collector, command string
		occurrence               int
		partial                  bool
	}{
		{"bgp VRFs", "bgp", "show bgp vrf all json", 1, false},
		{"bgp neighbors", "bgp", "show bgp vrf default neighbors json", 1, false},
		{"bfd peer VRFs", "bfd", "show bgp vrf all json", 1, false},
		{"bfd peers", "bfd", "show bfd vrf default peers json", 1, false},
		{"bfd counter VRFs", "bfd", "show bgp vrf all json", 2, true},
		{"bfd counters", "bfd", "show bfd vrf default peers counters json", 1, true},
		{"evpn VNIs", "evpn", "show evpn vni json", 1, false},
	} {
		for _, failure := range []string{"command", "parse"} {
			t.Run(tt.name+"/"+failure, func(t *testing.T) {
				fail := true
				calls := 0
				cli := func(command string) (string, error) {
					if fail && command == tt.command {
						calls++
						if calls == tt.occurrence {
							if failure == "command" {
								return "", errors.New("read failed")
							}
							return "{", nil
						}
					}
					return scrapeCLI(command)
				}
				r := prometheus.NewPedanticRegistry()
				r.MustRegister(scrapeCollector(tt.collector, cli))
				families := checkScrape(t, r, tt.collector, 0)
				if calls != tt.occurrence {
					t.Fatalf("failure reached %d times, want %d", calls, tt.occurrence)
				}
				if tt.partial {
					if !families["frrk8s_bfd_session_up"] || families["frrk8s_bfd_control_packet_input"] {
						t.Fatalf("want partial BFD sessions without counters, got %v", families)
					}
				} else if len(families) != 1 {
					t.Fatalf("want only failure status, got %v", families)
				}
				fail = false
				if families := checkScrape(t, r, tt.collector, 1); len(families) == 1 {
					t.Fatal("recovered scrape omitted observations")
				}
				// A later failure must not retain success from recovery.
				fail, calls = true, 0
				checkScrape(t, r, tt.collector, 0)
			})
		}
	}
}

func TestScrapeEmptyInventory(t *testing.T) {
	for _, tt := range []struct {
		name, collector, command, output string
	}{
		{"bgp no VRFs", "bgp", "show bgp vrf all json", "{}"},
		{"bgp no neighbors", "bgp", "show bgp vrf default neighbors json", "{}"},
		{"bfd no VRFs", "bfd", "show bgp vrf all json", "{}"},
		{"bfd no peers", "bfd", "", "[]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cli := func(command string) (string, error) {
				if command == tt.command || (tt.command == "" && strings.HasPrefix(command, "show bfd")) {
					return tt.output, nil
				}
				return scrapeCLI(command)
			}
			r := prometheus.NewPedanticRegistry()
			r.MustRegister(scrapeCollector(tt.collector, cli))
			if families := checkScrape(t, r, tt.collector, 1); len(families) != 1 {
				t.Fatalf("want only success status for empty inventory, got %v", families)
			}
		})
	}
}

func TestScrapeJointRegistration(t *testing.T) {
	r := prometheus.NewPedanticRegistry()
	for _, name := range []string{"bgp", "bfd", "evpn"} {
		if err := r.Register(scrapeCollector(name, scrapeCLI)); err != nil {
			t.Fatalf("registering %s: %v", name, err)
		}
	}
	expected := scrapeSuccessText("bfd", 1) + "frrk8s_scrape_success{collector=\"bgp\"} 1\nfrrk8s_scrape_success{collector=\"evpn\"} 1\n"
	if err := testutil.GatherAndCompare(r, strings.NewReader(expected), "frrk8s_scrape_success"); err != nil {
		t.Fatal(err)
	}
}

func scrapeCollector(name string, cli vtysh.Cli) prometheus.Collector {
	l := log.NewNopLogger()
	switch name {
	case "bgp":
		c := NewBGP(l).(*bgp)
		c.frrCli = cli
		return c
	case "bfd":
		c := NewBFD(l).(*bfd)
		c.frrCli = cli
		return c
	case "evpn":
		c := NewEVPN(l).(*evpn)
		c.frrCli = cli
		return c
	default:
		panic("unknown collector " + name)
	}
}

func scrapeCLI(command string) (string, error) {
	switch command {
	case "show bgp vrf all json":
		return `{"default":{}}`, nil
	case "show bgp vrf default neighbors json":
		return neighborsIPv4Only, nil
	case "show bfd vrf default peers json":
		return peersIPv4, nil
	case "show bfd vrf default peers counters json":
		return peersCountersIPv4, nil
	case "show evpn vni json":
		return evpnCapture, nil
	default:
		return "", fmt.Errorf("unexpected command %q", command)
	}
}

func scrapeSuccessText(collector string, success int) string {
	return fmt.Sprintf(`# HELP frrk8s_scrape_success 1 if all collector reads and parses succeeded, 0 otherwise
# TYPE frrk8s_scrape_success gauge
frrk8s_scrape_success{collector=%q} %d
`, collector, success)
}

func checkScrape(t *testing.T, r *prometheus.Registry, collector string, success int) map[string]bool {
	t.Helper()
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	cached := prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) { return families, nil })
	if err := testutil.GatherAndCompare(cached, strings.NewReader(scrapeSuccessText(collector, success)), "frrk8s_scrape_success"); err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, family := range families {
		names[family.GetName()] = true
	}
	return names
}
