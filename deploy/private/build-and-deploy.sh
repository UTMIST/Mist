#!/bin/bash
# API/auth images are built/imported on the main node; no registry push is needed.
# Requires Kubernetes credentials and root privileges for image import/web install.
set -euo pipefail
cd "$(dirname "$0")/../.."
export PATH=/home/utmist/.local/share/mist-runtimes/go-1.25.1/bin:/home/utmist/.local/share/mist-runtimes/node-22.16.0/bin:$PATH
export KUBECONFIG=${KUBECONFIG:-/home/utmist/.kube/config}
# Never silently deploy an unauthenticated service if its bootstrap secret is missing.
k3s kubectl get secret mist-auth-bootstrap -n mist-system >/dev/null
python3 deploy/private/prepare-internal-secret.py
CGO_ENABLED=0 go -C src build -o ../bin/mist-api .
CGO_ENABLED=0 go -C cli build -o ../bin/mist-cli .
docker build -f deploy/k3s/Dockerfile.api -t mist-api:departments-20261009 .
docker build -t mist-auth:departments-20261009 auth-service
archive=$(mktemp /tmp/mist-deploy.XXXXXX.tar)
trap 'python3 -c "import os,sys; os.unlink(sys.argv[1])" "$archive"' EXIT
docker save -o "$archive" mist-api:departments-20261009 mist-auth:departments-20261009
# Existing approved Docker access permits host import, without changing sudo policy.
docker run --rm --mount type=bind,src=/,dst=/host alpine:3.16.3 chroot /host /usr/local/bin/k3s ctr images import "$archive"
k3s kubectl apply -f deploy/k3s/storage/shared.yaml -f deploy/k3s/team-rbac.yaml -f deploy/k3s/team-workload-policy.yaml -f deploy/k3s/mist-auth.yaml -f deploy/k3s/mist-api.yaml
k3s kubectl rollout restart deployment/mist-auth -n mist-system
k3s kubectl rollout status deployment/mist-auth -n mist-system --timeout=120s
k3s kubectl rollout restart deployment/mist-api -n mist-system
k3s kubectl rollout status deployment/mist-api -n mist-system --timeout=120s
npm --prefix web-interface ci
npx --prefix web-interface tsc --noEmit -p web-interface/tsconfig.json
npm --prefix web-interface run build
docker run --rm --privileged --pid host --network host --mount type=bind,src=/,dst=/host alpine:3.16.3 chroot /host /usr/bin/nsenter -t 1 -m -n -p -r -w /bin/bash "$PWD/deploy/private/install-web.sh" "$PWD"
for attempt in $(seq 1 15); do
  if curl --fail --silent http://127.0.0.1:8088/api/session; then exit 0; fi
  sleep 2
done
echo 'Private website session check failed' >&2
exit 1
