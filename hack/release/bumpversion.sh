#!/bin/bash
set -e

if [ -z "$FRRK8S_VERSION" ]; then
    echo "must set the FRRK8S_VERSION environment variable"
    exit -1
fi

sed -i "s/newTag:.*$/newTag: v$FRRK8S_VERSION/" config/frr-k8s/kustomization.yaml

sed -i "s/version:.*$/version: $FRRK8S_VERSION/" charts/frr-k8s/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$FRRK8S_VERSION/" charts/frr-k8s/Chart.yaml
sed -i "s/version:.*$/version: $FRRK8S_VERSION/" charts/frr-k8s/charts/crds/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$FRRK8S_VERSION/" charts/frr-k8s/charts/crds/Chart.yaml
helm dep update charts/frr-k8s

sed -i "s/version.*release cutting.*$/version = \"v$FRRK8S_VERSION\"/" internal/version/version.go
gofmt -w internal/version/version.go
