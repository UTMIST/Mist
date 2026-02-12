#!/bin/bash
# Run CPU-only container tests. Requires Docker and Redis.
# Usage: ./run_container_job_test.sh

set -e
cd "$(dirname "$0")/.."

echo "Building pytorch-cpu image..."
docker build -t pytorch-cpu ./images/pytorch-cpu || {
  echo "Failed to build pytorch-cpu. Ensure Docker is running."
  exit 1
}

echo "Starting Redis..."
docker compose up -d redis 2>/dev/null || docker-compose up -d redis 2>/dev/null || true
sleep 2

echo "Running container job tests..."
go test -v -run 'TestContainerJobCPU|TestRunContainerCPUIntegration' -count=1

go test -v -run TestRunContainerCPU -count=1

echo "All container job tests passed."
