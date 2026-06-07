#!/usr/bin/env bash
#
# Starts the Interview-Guide demo: 4 specialist agents + 1 orchestrator, each
# behind an AgentMesh sidecar running the A2A data plane.
#
# Prerequisite: the control plane must already be running (gRPC on :9091).
# See README.md.

set -euo pipefail
cd "$(dirname "$0")"
REPO_ROOT="$(cd ../.. && pwd)"

CP_ADDR="${CONTROL_PLANE_ADDR:-localhost:9091}"
OTLP="${OTLP_ENDPOINT:-localhost:4317}"

PIDS=()
cleanup() {
  echo
  echo "stopping demo..."
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

echo "building sidecar binary..."
( cd "$REPO_ROOT" && go build -o bin/sidecar ./cmd/sidecar )
SIDECAR="$REPO_ROOT/bin/sidecar"

# start <agent-id> <capability> <app-port> <inbound-port> <app-file> <mesh-api-addr>
start() {
  local name="$1" cap="$2" app_port="$3" inbound="$4" app="$5" mesh="${6:-}"
  python3 "agents/$app" & PIDS+=($!)
  sleep 0.3
  "$SIDECAR" \
    --data-plane a2a \
    --control-plane-addr "$CP_ADDR" \
    --otlp-endpoint "$OTLP" \
    --agent-id "$name" \
    --capabilities "$cap" \
    --endpoint "http://127.0.0.1:$inbound" \
    --listen-addr "127.0.0.1:$inbound" \
    --forward-to-url "http://127.0.0.1:$app_port/invoke" \
    --mesh-api-addr "$mesh" & PIDS+=($!)
  sleep 0.4
}

echo "starting specialist agents + sidecars..."
start persona-1      persona         8001 9101 persona.py      ""
start questions-1    questions       8002 9102 questions.py    ""
start critic-1       critique        8003 9103 critic.py       ""
start compiler-1     compile         8004 9104 compiler.py     ""

echo "starting orchestrator (outbound mesh API on 127.0.0.1:9099)..."
start orchestrator-1 interview-guide 8000 9100 orchestrator.py 127.0.0.1:9099

cat <<EOF

All agents are up. Trigger the demo in another terminal with:

  $SIDECAR --data-plane a2a --control-plane-addr $CP_ADDR \\
    --invoke-capability interview-guide \\
    --invoke-payload '{"role":"Backend Engineer"}' --invoke-from demo

Ctrl+C here to stop everything.
EOF

wait
