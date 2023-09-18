// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	Namespace = "frrk8s"
	Subsystem = "k8s_client"

	updates = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "updates_total",
		Help:      "Number of k8s object updates that have been processed.",
	})

	updateErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "update_errors_total",
		Help:      "Number of k8s object updates that failed for some reason.",
	})

	configLoaded = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "config_loaded_bool",
		Help:      "1 if the MetalLB configuration was successfully loaded at least once.",
	})

	configStale = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "config_stale_bool",
		Help:      "1 if running on a stale configuration, because the latest config failed to load.",
	})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(updates, updateErrors, configLoaded, configStale)
}
