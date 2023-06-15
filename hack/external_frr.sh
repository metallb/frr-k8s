#!/bin/bash
set -x

NODES=$(kubectl get nodes -l node-role.kubernetes.io/worker=worker -o jsonpath={.items[*].status.addresses[?\(@.type==\"InternalIP\"\)].address})
echo $NODES
pushd ./hack/frr/
go run . -nodes "$NODES"
popd

FRR_CONFIG=$(mktemp -d -t frr-XXXXXXXXXX)
cp hack/frr/*.conf $FRR_CONFIG
cp hack/frr/daemons $FRR_CONFIG
chmod a+rw $FRR_CONFIG/*

docker rm -f frr
docker run -d --privileged --network kind --rm --ulimit core=-1 --name frr --volume "$FRR_CONFIG":/etc/frr quay.io/frrouting/frr:8.4.2

FRR_IP=$(docker inspect -f "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}" frr)


cat <<EOF | kubectl apply -f -
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: frrconfiguration-sample
  namespace: default
spec:
  bgp:
    routers:
    - asn: 64512
      prefixes:
      - 192.168.5.0/24
      neighbors:
      - asn: 64512
        address: $FRR_IP
        toAdvertise:
          allowed:
            prefixes:
            - 192.168.5.0/24
EOF
