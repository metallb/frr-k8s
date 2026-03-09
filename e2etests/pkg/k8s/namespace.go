// SPDX-License-Identifier:Apache-2.0

package k8s

var (
	FRRK8sNamespace       = "frr-k8s-system"
	FRRK8sDefaultLogLevel = "debug"
)

const (
	FRRK8sDaemonsetLS                = "app.kubernetes.io/component=frr-k8s"
	FRRK8sStatusCleanerApp           = "app.kubernetes.io/component=statuscleaner"
	FRRK8SContainerName              = "controller"
	FRRK8SStatusContainerName        = "frr-status"
	FRRContainerName                 = "frr"
	FRRK8SStatusCleanerContainerName = "frr-k8s-statuscleaner"
	FRRK8sConfigurationName          = "config"
)
