// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/metallb/frr-k8s/frr-tools/metrics/vtysh"
)

var labels = []string{"peer", "vrf"}

var (
	sessionUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, SessionUp.Name),
		SessionUp.Help,
		labels,
		nil,
	)

	prefixesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, Prefixes.Name),
		Prefixes.Help,
		labels,
		nil,
	)

	receivedPrefixesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, ReceivedPrefixes.Name),
		ReceivedPrefixes.Help,
		labels,
		nil,
	)

	opensSentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "opens_sent"),
		"Number of BGP open messages sent",
		labels,
		nil,
	)

	opensReceivedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "opens_received"),
		"Number of BGP open messages received",
		labels,
		nil,
	)

	notificationsSentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "notifications_sent"),
		"Number of BGP notification messages sent",
		labels,
		nil,
	)

	updatesSentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, UpdatesSent.Name),
		UpdatesSent.Help,
		labels,
		nil,
	)

	updatesReceivedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "updates_total_received"),
		"Number of BGP UPDATE messages received",
		labels,
		nil,
	)

	keepalivesSentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "keepalives_sent"),
		"Number of BGP keepalive messages sent",
		labels,
		nil,
	)

	keepalivesReceivedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "keepalives_received"),
		"Number of BGP keepalive messages received",
		labels,
		nil,
	)

	routeRefreshSentedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "route_refresh_sent"),
		"Number of BGP route refresh messages sent",
		labels,
		nil,
	)

	totalSentDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "total_sent"),
		"Number of total BGP messages sent",
		labels,
		nil,
	)

	totalReceivedDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, Subsystem, "total_received"),
		"Number of total BGP messages received",
		labels,
		nil,
	)
)

type bgp struct {
	Log    log.Logger
	frrCli vtysh.Cli
}

func NewBGP(l log.Logger) prometheus.Collector {
	log := log.With(l, "collector", Subsystem)
	return &bgp{Log: log, frrCli: vtysh.Run}
}

func mocknewBGP(l log.Logger) *bgp {
	log := log.With(l, "collector", Subsystem)
	return &bgp{Log: log, frrCli: vtysh.Run}
}

func (c *bgp) Describe(ch chan<- *prometheus.Desc) {
	ch <- sessionUpDesc
	ch <- prefixesDesc
	ch <- receivedPrefixesDesc
	ch <- opensSentDesc
	ch <- opensReceivedDesc
	ch <- notificationsSentDesc
	ch <- updatesSentDesc
	ch <- updatesReceivedDesc
	ch <- keepalivesSentDesc
	ch <- keepalivesReceivedDesc
	ch <- routeRefreshSentedDesc
	ch <- totalSentDesc
	ch <- totalReceivedDesc
}

func (c *bgp) Collect(ch chan<- prometheus.Metric) {
	neighbors, err := vtysh.GetBGPNeighbors(c.frrCli)
	if err != nil {
		level.Error(c.Log).Log("error", err, "msg", "failed to fetch BGP neighbors from FRR")
		return
	}

	updateNeighborsMetrics(ch, neighbors)
}

func updateNeighborsMetrics(ch chan<- prometheus.Metric, neighbors map[string][]*frr.Neighbor) {
	for vrf, nn := range neighbors {
		for _, n := range nn {
			sessionUp := 1
			if !n.Connected {
				sessionUp = 0
			}
			peerLabel := n.MetricName()

			ch <- prometheus.MustNewConstMetric(sessionUpDesc, prometheus.GaugeValue, float64(sessionUp), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(prefixesDesc, prometheus.GaugeValue, float64(n.PrefixSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(receivedPrefixesDesc, prometheus.GaugeValue, float64(n.PrefixReceived), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(opensSentDesc, prometheus.CounterValue, float64(n.MsgStats.OpensSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(opensReceivedDesc, prometheus.CounterValue, float64(n.MsgStats.OpensReceived), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(notificationsSentDesc, prometheus.CounterValue, float64(n.MsgStats.NotificationsSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(updatesSentDesc, prometheus.CounterValue, float64(n.MsgStats.UpdatesSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(updatesReceivedDesc, prometheus.CounterValue, float64(n.MsgStats.UpdatesReceived), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(keepalivesSentDesc, prometheus.CounterValue, float64(n.MsgStats.KeepalivesSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(keepalivesReceivedDesc, prometheus.CounterValue, float64(n.MsgStats.KeepalivesReceived), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(routeRefreshSentedDesc, prometheus.CounterValue, float64(n.MsgStats.RouteRefreshSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(totalSentDesc, prometheus.CounterValue, float64(n.MsgStats.TotalSent), peerLabel, vrf)
			ch <- prometheus.MustNewConstMetric(totalReceivedDesc, prometheus.CounterValue, float64(n.MsgStats.TotalReceived), peerLabel, vrf)
		}
	}
}
