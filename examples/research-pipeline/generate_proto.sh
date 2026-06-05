#!/usr/bin/env bash
# Generate Python gRPC stubs from the AgentMesh protos into ./lib so the
# planner can import them.
set -euo pipefail

cd "$(dirname "$0")"

PROTO_DIR="../../proto"
OUT_DIR="./lib"

mkdir -p "$OUT_DIR"

python -m grpc_tools.protoc \
    -I "$PROTO_DIR" \
    --python_out="$OUT_DIR" \
    --grpc_python_out="$OUT_DIR" \
    "$PROTO_DIR/agentmesh/v1/control_plane.proto" \
    "$PROTO_DIR/agentmesh/v1/data_plane.proto"

# Ensure generated packages are importable.
touch "$OUT_DIR/agentmesh/__init__.py"
touch "$OUT_DIR/agentmesh/v1/__init__.py"

echo "Generated stubs in $OUT_DIR/agentmesh/v1/"
ls -la "$OUT_DIR/agentmesh/v1/" | grep -v __pycache__
