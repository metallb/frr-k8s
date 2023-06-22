#!/usr/bin/env bash
set -o errexit

KIND_BIN="${KIND_BIN:-kind}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-frr-k8s}"
IP_FAMILY="${IP_FAMILY:-ipv4}"
NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.26.0}"

clusters=$("${KIND_BIN}" get clusters)
for cluster in $clusters; do
  if [[ $cluster == "$KIND_CLUSTER_NAME" ]]; then
    echo "Cluster ${KIND_CLUSTER_NAME} already exists"
    exit 0
  fi
done

# create a cluster with the local registry enabled in containerd
cat <<EOF | "${KIND_BIN}" create cluster --image "${NODE_IMAGE}" --name "${KIND_CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  ipFamily: "${IP_FAMILY}"
nodes:
- role: control-plane
- role: worker
- role: worker
EOF

kubectl label node "$KIND_CLUSTER_NAME"-worker "$KIND_CLUSTER_NAME"-worker2 node-role.kubernetes.io/worker=worker
