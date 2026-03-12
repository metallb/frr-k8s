# frr-k8s

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.0.0](https://img.shields.io/badge/AppVersion-v0.0.0-informational?style=flat-square)

A cloud native wrapper of FRR

**Homepage:** <https://metallb.universe.tf>

## Source Code

* <https://github.com/metallb/frr-k8s>

## Requirements

Kubernetes: `>= 1.19.0-0`

| Repository | Name | Version |
|------------|------|---------|
|  | crds | 0.0.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.enabled | bool | `true` |  |
| crds.validationFailurePolicy | string | `"Fail"` |  |
| frrk8s.affinity | object | `{}` |  |
| frrk8s.alwaysBlock | string | `""` | A comma separated list of cidrs to always block for incoming routes. |
| frrk8s.disableCertRotation | bool | `false` | Specifies whether the cert rotator works as part of the webhook. |
| frrk8s.frr.acceptIncomingBGPConnections | bool | `false` |  |
| frrk8s.frr.image.pullPolicy | string | `nil` |  |
| frrk8s.frr.image.repository | string | `"quay.io/frrouting/frr"` |  |
| frrk8s.frr.image.tag | string | `"10.4.1"` |  |
| frrk8s.frr.metricsBindAddress | string | `"127.0.0.1"` |  |
| frrk8s.frr.metricsPort | int | `7573` |  |
| frrk8s.frr.resources | object | `{}` |  |
| frrk8s.frr.secureMetricsPort | int | `9141` |  |
| frrk8s.frrMetrics.resources | object | `{}` |  |
| frrk8s.frrStatus.pollInterval | string | `"2m"` |  |
| frrk8s.frrStatus.resources | object | `{}` |  |
| frrk8s.image.pullPolicy | string | `nil` |  |
| frrk8s.image.repository | string | `"quay.io/metallb/frr-k8s"` |  |
| frrk8s.image.tag | string | `nil` |  |
| frrk8s.labels.app | string | `"frr-k8s"` |  |
| frrk8s.livenessProbe.enabled | bool | `true` |  |
| frrk8s.livenessProbe.failureThreshold | int | `3` |  |
| frrk8s.livenessProbe.initialDelaySeconds | int | `10` |  |
| frrk8s.livenessProbe.periodSeconds | int | `10` |  |
| frrk8s.livenessProbe.successThreshold | int | `1` |  |
| frrk8s.livenessProbe.timeoutSeconds | int | `1` |  |
| frrk8s.logLevel | string | `"info"` | Controller log level that is passed as a CLI flag. Must be one of: `all`, `debug`, `info`, `warn`, `error` or `none` |
| frrk8s.nodeSelector | object | `{}` |  |
| frrk8s.podAnnotations | object | `{}` |  |
| frrk8s.priorityClassName | string | `""` |  |
| frrk8s.readinessProbe.enabled | bool | `true` |  |
| frrk8s.readinessProbe.failureThreshold | int | `3` |  |
| frrk8s.readinessProbe.initialDelaySeconds | int | `10` |  |
| frrk8s.readinessProbe.periodSeconds | int | `10` |  |
| frrk8s.readinessProbe.successThreshold | int | `1` |  |
| frrk8s.readinessProbe.timeoutSeconds | int | `1` |  |
| frrk8s.reloader.resources | object | `{}` |  |
| frrk8s.resources | object | `{}` |  |
| frrk8s.restartOnRotatorSecretRefresh | bool | `false` | Specifies whether the pod restarts when the rotator refreshes the cert secret. Useful for webhook stability during redeployments. |
| frrk8s.runtimeClassName | string | `""` |  |
| frrk8s.serviceAccount.annotations | object | `{}` |  |
| frrk8s.serviceAccount.create | bool | `true` | Specifies whether a ServiceAccount should be created. |
| frrk8s.serviceAccount.name | string | `""` | The name of the ServiceAccount to use. If not set and create is true, a name is generated using the fullname template. |
| frrk8s.startupProbe.enabled | bool | `true` |  |
| frrk8s.startupProbe.failureThreshold | int | `30` |  |
| frrk8s.startupProbe.periodSeconds | int | `5` |  |
| frrk8s.tolerateMaster | bool | `true` |  |
| frrk8s.tolerations | list | `[]` |  |
| frrk8s.updateStrategy.type | string | `"RollingUpdate"` |  |
| frrk8s.webhookPort | int | `19443` |  |
| fullnameOverride | string | `""` |  |
| nameOverride | string | `""` |  |
| prometheus.metricsBindAddress | string | `"127.0.0.1"` | Bind address frr-k8s will use for metrics. |
| prometheus.metricsPort | int | `7572` | Port frr-k8s will listen on for metrics. |
| prometheus.metricsTLSSecret | string | `""` | The name of the secret to be mounted in the frr-k8s pod to expose metrics securely. If not present, a self-signed certificate will be used. |
| prometheus.namespace | string | `""` | The namespace where Prometheus is deployed. Required when ".Values.prometheus.rbacPrometheus == true" and "prometheus.serviceMonitor.enabled=true". |
| prometheus.rbacPrometheus | bool | `false` | Give Prometheus permission to scrape metallb's namespace. |
| prometheus.rbacProxy.pullPolicy | string | `nil` |  |
| prometheus.rbacProxy.repository | string | `"gcr.io/kubebuilder/kube-rbac-proxy"` |  |
| prometheus.rbacProxy.tag | string | `"v0.12.0"` |  |
| prometheus.scrapeAnnotations | bool | `false` | Add Prometheus metric auto-collection annotations to pods. |
| prometheus.secureMetricsPort | int | `9140` | If set, enables rbac proxy on frr-k8s to expose the metrics via TLS. |
| prometheus.serviceAccount | string | `""` | The service account used by Prometheus. Required when ".Values.prometheus.rbacPrometheus == true" and "prometheus.serviceMonitor.enabled=true" |
| prometheus.serviceMonitor.additionalLabels | object | `{}` |  |
| prometheus.serviceMonitor.annotations | object | `{}` | Optional additional annotations for the controller serviceMonitor. |
| prometheus.serviceMonitor.enabled | bool | `false` | Enable support for Prometheus Operator. |
| prometheus.serviceMonitor.interval | string | `nil` | Scrape interval. If not set, the Prometheus default scrape interval is used. |
| prometheus.serviceMonitor.jobLabel | string | `"app.kubernetes.io/name"` | Job label for scrape target. |
| prometheus.serviceMonitor.metricRelabelings | list | `[]` | Metric relabel configs to apply to samples before ingestion. |
| prometheus.serviceMonitor.relabelings | list | `[]` | Relabel configs to apply to samples before ingestion. |
| prometheus.serviceMonitor.tlsConfig.insecureSkipVerify | bool | `true` | Disables SSL certificate verification |
| rbac.create | bool | `true` | Specifies whether to install and use RBAC rules. |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.10.0](https://github.com/norwoodj/helm-docs/releases/v1.10.0)
