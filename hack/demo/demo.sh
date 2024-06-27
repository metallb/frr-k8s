#!/bin/bash
set -x

NODES=$(kubectl get nodes -o jsonpath={.items[*].status.addresses[?\(@.type==\"InternalIP\"\)].address})
echo $NODES
pushd ./frr/
go run . -nodes "$NODES"
popd

FRR_CONFIG=$(mktemp -d -t frr-XXXXXXXXXX)
cp frr/*.conf $FRR_CONFIG
cp frr/daemons $FRR_CONFIG
chmod a+rw $FRR_CONFIG/*

docker rm -f frr
docker run -d --privileged --network kind --rm --ulimit core=-1 --name frr --volume "$FRR_CONFIG":/etc/frr quay.io/frrouting/frr:9.1.0

FRR_IP=$(docker inspect -f "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}" frr)

echo "external FRR IP is $FRR_IP"

for i in configs/*.yaml; do
	rm $i
done

cp configs/templates/*.yaml configs/

for i in configs/*.yaml; do
    sed -i "s/NEIGHBOR_IP/$FRR_IP/g" $i
done

echo "Setup is complete, demo yamls can be found in $(pwd)/configs"
