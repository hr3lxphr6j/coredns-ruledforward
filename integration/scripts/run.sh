#!/bin/sh
# Host script: build images, start stack, run integration tests in client container, tear down.
# Run from repo root: ./integration/scripts/run.sh
set -e

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "$REPO_ROOT"

# Download dlc.dat if missing (v2fly domain-list-community)
DLC_URL="https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
if [ ! -f "integration/fixtures/dlc.dat" ]; then
  echo "Downloading integration/fixtures/dlc.dat from $DLC_URL ..."
  mkdir -p integration/fixtures
  if command -v curl >/dev/null 2>&1; then
    curl -fSL -o integration/fixtures/dlc.dat "$DLC_URL"
  else
    wget -q -O integration/fixtures/dlc.dat "$DLC_URL"
  fi
fi

cd integration
echo "Building Docker images..."
docker compose build

echo "Starting services..."
docker compose up -d stub coredns
# Give stub and coredns time to start and load (dlc.dat ~2MB can take several seconds)
sleep 8

# Run client as part of compose stack (same network, "coredns" hostname resolves via Docker DNS)
rc=0
echo "Running integration tests in client container..."
docker compose up --abort-on-container-exit --exit-code-from client client || rc=$?

echo "Stopping services..."
docker compose down

exit $rc
