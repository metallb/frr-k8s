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
)

func TestScrapeFailureAndRecovery(t *testing.T) {
	for _, tt := range []struct {
		name, collector, command string
		partial, dropped         string // series that survive or go with a partial failure
	}{
		{"bgp", "bgp", "show bgp vrf default neighbors json", "", ""},
		{"bfd", "bfd", "show bfd vrf default peers json", "", ""},
		{"bfd counters", "bfd", "show bfd vrf default peers counters json", "frrk8s_bfd_session_up", "frrk8s_bfd_control_packet_input"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fail := true
			c := scrapeCollector(tt.collector, func(command string) (string, error) {
				if fail && command == tt.command {
					return "", errors.New("read failed")
				}
				return scrapeCLI(command)
			})
			checkScrape(t, c, tt.collector, 0)
			if tt.partial == "" && testutil.CollectAndCount(c) != 1 {
				t.Fatal("want only the failure status")
			}
			if tt.partial != "" && (testutil.CollectAndCount(c, tt.partial) == 0 || testutil.CollectAndCount(c, tt.dropped) != 0) {
				t.Fatalf("want %s and no %s after a partial failure", tt.partial, tt.dropped)
			}
			fail = false
			checkScrape(t, c, tt.collector, 1)
		})
	}
}

func TestScrapeEmptyInventory(t *testing.T) {
	for _, tt := range []struct {
		collector, command string
	}{
		{"bgp", "show bgp vrf default neighbors json"},
		{"bfd", "show bgp vrfs json"},
	} {
		t.Run(tt.collector, func(t *testing.T) {
			c := scrapeCollector(tt.collector, func(command string) (string, error) {
				if command == tt.command {
					return "{}", nil
				}
				return scrapeCLI(command)
			})
			checkScrape(t, c, tt.collector, 1)
			if n := testutil.CollectAndCount(c); n != 1 {
				t.Fatalf("want only the success status, got %d series", n)
			}
		})
	}
}

func TestScrapeJointRegistration(t *testing.T) {
	r := prometheus.NewPedanticRegistry()
	for _, name := range []string{"bgp", "bfd"} {
		if err := r.Register(scrapeCollector(name, scrapeCLI)); err != nil {
			t.Fatalf("registering %s: %v", name, err)
		}
	}
	expected := scrapeSuccessText("bfd", 1) + "frrk8s_scrape_success{collector=\"bgp\"} 1\n"
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
	default:
		panic("unknown collector " + name)
	}
}

func scrapeCLI(command string) (string, error) {
	switch command {
	case "show bgp vrfs json":
		return `{"vrfs":{"default":{}}}`, nil
	case "show bgp vrf default neighbors json":
		return neighborsIPv4Only, nil
	case "show bfd vrf default peers json":
		return peersIPv4, nil
	case "show bfd vrf default peers counters json":
		return peersCountersIPv4, nil
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

func checkScrape(t *testing.T, c prometheus.Collector, collector string, success int) {
	t.Helper()
	if err := testutil.CollectAndCompare(c, strings.NewReader(scrapeSuccessText(collector, success)), "frrk8s_scrape_success"); err != nil {
		t.Fatal(err)
	}
}
