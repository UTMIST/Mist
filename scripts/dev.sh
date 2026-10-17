#!/usr/bin/env bash
#
# scripts/dev.sh - Bring up local development environment for Mist
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Text styles
BOLD='\033[1m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_ok() { echo -e "${GREEN}[OK]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

usage() {
  echo -e "${BOLD}Usage:${NC} $0 [options]

${BOLD}Options:${NC}
  --frontend-only       Run only the web interface dev server (Vite)
  --port-forward        Forward Kubernetes mist-api service to localhost:3000 and run frontend
  --full-stack          Start local Auth service, Backend API, and Frontend dev server
  --k3s                 Start a local k3s server instance
  --port <port>         Frontend dev server port (default: 3001)
  --api-proxy <url>     Set backend API proxy URL (default: http://127.0.0.1:3000)
  --help, -h            Show this help message

${BOLD}Examples:${NC}
  $0                    Auto-detect cluster and start local dev environment
  $0 --port-forward     Forward deployed cluster API (port 3000) & start frontend (port 3001)
  $0 --api-proxy http://100.73.139.66:8088/api"
  exit 0
}

# Parse options
MODE="auto"
FRONTEND_PORT="3001"
API_PROXY="${MIST_API_PROXY:-http://127.0.0.1:3000}"
START_K3S=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --frontend-only)
      MODE="frontend"
      shift
      ;;
    --port-forward)
      MODE="port-forward"
      shift
      ;;
    --full-stack)
      MODE="full-stack"
      shift
      ;;
    --k3s)
      START_K3S=true
      shift
      ;;
    --port)
      FRONTEND_PORT="$2"
      shift 2
      ;;
    --api-proxy)
      API_PROXY="$2"
      shift 2
      ;;
    --help|-h)
      usage
      ;;
    *)
      log_error "Unknown option: $1"
      usage
      ;;
  esac
done

# Track background PIDs for cleanup
PIDS=()
cleanup() {
  if [ ${#PIDS[@]} -gt 0 ]; then
    log_info "Stopping background services..."
    for pid in "${PIDS[@]}"; do
      if kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
      fi
    done
    wait 2>/dev/null || true
  fi
  log_ok "All services stopped."
}
trap cleanup EXIT INT TERM

# Ensure dependencies exist
check_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "Command '$1' not found. Please activate the dev environment using 'direnv' or 'nix develop'."
    exit 1
  fi
}

check_command node
check_command npm
check_command go

# Discover KUBECONFIG if not set
find_kubeconfig() {
  if [ -n "${KUBECONFIG:-}" ] && [ -f "$KUBECONFIG" ]; then
    return 0
  fi
  for path in \
    "/home/utmist/.kube/config" \
    "$HOME/.kube/config" \
    "/etc/rancher/k3s/k3s.yaml" \
    "$HOME/.config/rancher/k3s/k3s.yaml"; do
    if [ -r "$path" ]; then
      export KUBECONFIG="$path"
      log_info "Found Kubeconfig at: $KUBECONFIG"
      return 0
    fi
  done
  return 1
}

# Check cluster reachability
cluster_reachable() {
  if command -v kubectl >/dev/null 2>&1; then
    kubectl cluster-info >/dev/null 2>&1 && return 0
  fi
  if command -v k3s >/dev/null 2>&1; then
    k3s kubectl cluster-info >/dev/null 2>&1 && return 0
  fi
  return 1
}

KUBECTL_CMD="kubectl"
if ! command -v kubectl >/dev/null 2>&1 && command -v k3s >/dev/null 2>&1; then
  KUBECTL_CMD="k3s kubectl"
fi

# Start local k3s if requested
if [ "$START_K3S" = true ]; then
  log_info "Starting local k3s server..."
  check_command k3s
  K3S_DATA_DIR="$ROOT_DIR/.k3s-dev"
  mkdir -p "$K3S_DATA_DIR"
  
  if [ "$(id -u)" -eq 0 ]; then
    k3s server --data-dir "$K3S_DATA_DIR" &
  else
    log_info "Running k3s in rootless mode..."
    k3s server --rootless --data-dir "$K3S_DATA_DIR" &
  fi
  PIDS+=($!)
  
  export KUBECONFIG="$HOME/.kube/k3s.yaml"
  log_info "Waiting for k3s cluster to be ready..."
  for _ in $(seq 1 30); do
    if cluster_reachable; then
      log_ok "Local k3s cluster is ready!"
      break
    fi
    sleep 2
  done
fi

# Auto-detect mode if "auto"
if [ "$MODE" = "auto" ]; then
  find_kubeconfig || true
  if cluster_reachable; then
    # Check if mist-api service exists in mist-system
    if $KUBECTL_CMD -n mist-system get service/mist-api >/dev/null 2>&1; then
      log_info "Detected active cluster with service/mist-api in mist-system."
      MODE="port-forward"
    else
      log_info "Detected Kubernetes cluster. Will use local full-stack mode with cluster connectivity."
      MODE="full-stack"
    fi
  else
    log_info "No reachable Kubernetes cluster found. Starting frontend with API proxy: $API_PROXY"
    MODE="frontend"
  fi
fi

# Setup web-interface dependencies
if [ ! -d "web-interface/node_modules" ]; then
  log_info "Installing web-interface dependencies..."
  npm --prefix web-interface ci
fi

# Execute according to mode
case "$MODE" in
  port-forward)
    log_info "Starting port-forward for service/mist-api on 127.0.0.1:3000..."
    $KUBECTL_CMD -n mist-system port-forward service/mist-api 3000:3000 --address 127.0.0.1 &
    PIDS+=($!)
    sleep 2

    log_info "Starting Vite frontend at http://127.0.0.1:${FRONTEND_PORT} (proxying to http://127.0.0.1:3000)..."
    export MIST_API_PROXY="http://127.0.0.1:3000"
    npm --prefix web-interface run dev -- --host 127.0.0.1 --port "$FRONTEND_PORT" --strictPort
    ;;

  full-stack)
    # 1. Start auth-service if dependencies are present
    if [ ! -d "auth-service/node_modules" ]; then
      log_info "Installing auth-service dependencies..."
      npm --prefix auth-service ci
    fi

    log_info "Starting Auth service on 127.0.0.1:3002..."
    export BETTER_AUTH_SECRET="${BETTER_AUTH_SECRET:-mist-local-dev-secret-key-32-chars-long}"
    export MIST_ADMIN_EMAIL="${MIST_ADMIN_EMAIL:-admin@mist.local}"
    export MIST_ADMIN_PASSWORD="${MIST_ADMIN_PASSWORD:-mistadmin12345}"
    export PORT=3002
    export HOST=127.0.0.1
    node auth-service/server.mjs &
    PIDS+=($!)
    sleep 2

    # 2. Build and start API
    log_info "Building mist-api..."
    go -C src build -o ../bin/mist-api .
    log_info "Starting mist-api on 127.0.0.1:3000..."
    export MIST_HTTP_ADDR="127.0.0.1:3000"
    export MIST_AUTH_URL="http://127.0.0.1:3002"
    bin/mist-api &
    PIDS+=($!)
    sleep 2

    # 3. Start Frontend
    log_info "Starting Vite frontend at http://127.0.0.1:${FRONTEND_PORT}..."
    export MIST_API_PROXY="http://127.0.0.1:3000"
    npm --prefix web-interface run dev -- --host 127.0.0.1 --port "$FRONTEND_PORT" --strictPort
    ;;

  frontend)
    log_info "Starting Vite frontend at http://127.0.0.1:${FRONTEND_PORT} (proxying to ${API_PROXY})..."
    export MIST_API_PROXY="$API_PROXY"
    npm --prefix web-interface run dev -- --host 127.0.0.1 --port "$FRONTEND_PORT" --strictPort
    ;;
esac
