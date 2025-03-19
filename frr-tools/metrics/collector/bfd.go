// SPDX-License-Identifier:Apache-2.0

package collector

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/frr-tools/metrics/vtysh"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/prometheus/client_golang/prometheus"
)

const subsystem = "bfd"

var (
	bfdSessionUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, SessionUp.Name),
		"BFD session state (1 is up, 0 is down)",
		labels,
		nil,
	)

	controlPacketInputDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "control_packet_input"),
		"Number of received BFD control packets",
		labels,
		nil,
	)

	controlPacketOutputDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "control_packet_output"),
		"Number of sent BFD control packets",
		labels,
		nil,
	)

	echoPacketInputDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "echo_packet_input"),
		"Number of received BFD echo packets",
		labels,
		nil,
	)

	echoPacketOutputDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "echo_packet_output"),
		"Number of sent BFD echo packets",
		labels,
		nil,
	)

	sessionUpEventsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "session_up_events"),
		"Number of BFD session up events",
		labels,
		nil,
	)

	sessionDownEventsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "session_down_events"),
		"Number of BFD session down events",
		labels,
		nil,
	)

	zebraNotificationsDesc = prometheus.NewDesc(
		prometheus.BuildFQName(Namespace, subsystem, "zebra_notifications"),
		"Number of BFD zebra notifications",
		labels,
		nil,
	)
)

type bfd struct {
	Log    log.Logger
	frrCli vtysh.Cli
}

func NewBFD(l log.Logger) prometheus.Collector {
	log := log.With(l, "collector", subsystem)
	return &bfd{Log: log, frrCli: vtysh.Run}
}

func mockNewBFD(l log.Logger) *bfd {
	log := log.With(l, "collector", subsystem)
	return &bfd{Log: log, frrCli: vtysh.Run}
}

func (c *bfd) Describe(ch chan<- *prometheus.Desc) {
	ch <- bfdSessionUpDesc
	ch <- controlPacketInputDesc
	ch <- controlPacketOutputDesc
	ch <- echoPacketInputDesc
	ch <- echoPacketOutputDesc
	ch <- sessionUpEventsDesc
	ch <- sessionDownEventsDesc
	ch <- zebraNotificationsDesc
}

func (c *bfd) Collect(ch chan<- prometheus.Metric) {
	peers, err := vtysh.GetBFDPeers(c.frrCli)
	if err != nil {
		level.Error(c.Log).Log("error", err, "msg", "failed to fetch BFD peers from FRR")
		return
	}

	updatePeersMetrics(ch, peers)

	peersCounters, err := vtysh.GetBFDPeersCounters(c.frrCli)
	if err != nil {
		level.Error(c.Log).Log("error", err, "msg", "failed to fetch BFD peers counters from FRR")
		return
	}

	updatePeersCountersMetrics(ch, peersCounters)
}

func updatePeersMetrics(ch chan<- prometheus.Metric, peersPerVRF map[string][]frr.BFDPeer) {
	for vrf, peers := range peersPerVRF {
		for _, p := range peers {
			sessionUp := 1
			if p.Status != "up" {
				sessionUp = 0
			}

			ch <- prometheus.MustNewConstMetric(bfdSessionUpDesc, prometheus.GaugeValue, float64(sessionUp), p.Peer, vrf)
		}
	}
}

func updatePeersCountersMetrics(ch chan<- prometheus.Metric, peersCountersPerVRF map[string][]frr.BFDPeerCounters) {
	for vrf, peersCounters := range peersCountersPerVRF {
		for _, p := range peersCounters {
			ch <- prometheus.MustNewConstMetric(controlPacketInputDesc, prometheus.CounterValue, float64(p.ControlPacketInput), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(controlPacketOutputDesc, prometheus.CounterValue, float64(p.ControlPacketOutput), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(echoPacketInputDesc, prometheus.CounterValue, float64(p.EchoPacketInput), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(echoPacketOutputDesc, prometheus.CounterValue, float64(p.EchoPacketOutput), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(sessionUpEventsDesc, prometheus.CounterValue, float64(p.SessionUpEvents), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(sessionDownEventsDesc, prometheus.CounterValue, float64(p.SessionDownEvents), p.Peer, vrf)
			ch <- prometheus.MustNewConstMetric(zebraNotificationsDesc, prometheus.CounterValue, float64(p.ZebraNotifications), p.Peer, vrf)
		}
	}
}
