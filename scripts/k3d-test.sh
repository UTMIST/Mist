#!/usr/bin/env bash
# Runs the K3s executor integration tests on a local k3d cluster.
# Needs docker (without sudo), k3d and kubectl. Safe to re-run: it reuses
# the cluster if it exists. Delete it with: k3d cluster delete mist-test
set -euo pipefail

cluster=mist-test
k3s_image=rancher/k3s:v1.36.5-k3s1
node=k3d-$cluster-server-0
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$root"

if ! k3d cluster get "$cluster" >/dev/null 2>&1; then
  k3d cluster create "$cluster" --image "$k3s_image" --servers 1 --agents 0 --no-lb \
    --k3s-arg "--disable=traefik,servicelb,metrics-server,local-storage@server:0" --wait --timeout 240s
fi
ctx=k3d-$cluster

CGO_ENABLED=0 go build -o "$work/mist-job-entry" ./cmd/mist-job-entry
CGO_ENABLED=0 go build -o "$work/mistd" ./cmd/mistd
cp internal/executor/k3s/testdata/runtime/Dockerfile "$work/"
docker build -q -t mist-test-runtime:dev "$work" >/dev/null
k3d image import -c "$cluster" mist-test-runtime:dev >/dev/null
docker cp "$work/mistd" "$node:/mistd"

kubectl --context "$ctx" apply -f deploy/k3s/mist-jobs.yaml -f deploy/k3s/rbac.yaml >/dev/null
# A miniature /mist on the node, with the storage contract's modes.
docker exec "$node" sh -c '
  mkdir -p /mist/software /mist/teams/team-a/alice /mist/teams/team-b/carol
  chmod 755 /mist /mist/teams /mist/software
  chown 0:40001 /mist/teams/team-a && chmod 2750 /mist/teams/team-a
  chown 20001:40001 /mist/teams/team-a/alice && chmod 2750 /mist/teams/team-a/alice
  chown 0:40002 /mist/teams/team-b && chmod 2750 /mist/teams/team-b'

MIST_K3D_CONTEXT=$ctx MIST_K3D_NODE=$node \
  go test -tags k3d -count=1 -v -run K3d ./internal/executor/k3s "$@"
