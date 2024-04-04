# FRRK8s Release Notes

## Release v0.0.11

### New Features

- Add a field to the FRRConfiguration CRD to disable MP BGP for the given peer (#128, @AlinaSecret)

### Bug fixes

- Fix the case where merging an FRRConfiguration with no hold / keepalive / connect Time set with one where the time is set to default fails. (#120, @fedepaol)

## Release v0.0.10

### New features

- FRR: bump to 9.0.2, allow to peer with localhost ([PR 118](https://github.com/metallb/frr-k8s/pull/118))
- Add option to configure BGP connect time ([PR_119](https://github.com/metallb/frr-k8s/pull/119))

## Release v0.0.9

### Bug fixes
 - helm: namespace all namespaced resources ([PR 117](https://github.com/metallb/frr-k8s/pull/117))


## Release v0.0.8

### Features

- Support a --always-block parameter. The parameter accepts a list of comma separated cidrs to always block. This is useful to protect well known cidrs such as pods or clusterIPs. ([PR 88](https://github.com/metallb/frr-k8s/pull/88))
- Support restarting the webhook pod when the rotator updates its cert secret.([PR 100](https://github.com/metallb/frr-k8s/pull/100))
- Add a demo environment creation script ([PR 107](https://github.com/metallb/frr-k8s/pull/107))

- Remove the DesiredConfig field from the status API ([PR 110](https://github.com/metallb/frr-k8s/pull/110))

### Bug fixes

- FRRConfigurations from namespaces different than the one the daemon is deployed on were not validated with other resoureces. ([PR 91](https://github.com/metallb/frr-k8s/pull/91))

- Empty always-block flag was not parsed correctly. ([PR 95](https://github.com/metallb/frr-k8s/pull/95))

- helm: webhooks probes pointed to the wrong endpoints. ([PR 97](https://github.com/metallb/frr-k8s/pull/97))

### Chores

- helm: add an option to disable the webhook's cert rotation. ([PR 93](https://github.com/metallb/frr-k8s/pull/93))
- add a new logo!
- CI: add a MetalLB E2E lane. ([PR 99](https://github.com/metallb/frr-k8s/pull/99))
- CI: don't run auto-generated files checks on dependabot PRs ([PR 111](https://github.com/metallb/frr-k8s/pull/111))
- kubectl: don't download if cluster is not reacheable ([PR 112](https://github.com/metallb/frr-k8s/pull/112))

## Release v0.0.4

### Bug fixes

- Merging neighbors always failed when holdtime and keepalivetime were set ([PR #86](https://github.com/metallb/frr-k8s/pull/86)).

### Chores

- Enforce adding release notes in CI ([PR #79](https://github.com/metallb/frr-k8s/pull/79))
- helm: Fix metricRelabelings templating ([PR #83](https://github.com/metallb/frr-k8s/pull/83))

## Release v0.0.3

Support establishing BGP sessions with cleartext passwords ([PR #80](https://github.com/metallb/frr-k8s/pull/80)).

## Release v0.0.2

Helm charts: flip the prometheus service monitor default to false. Service Monitor should be opt-in for those users that have
the prometheus operator deployed.

## Release v0.0.1

First release!
