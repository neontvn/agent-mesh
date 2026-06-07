"""Minimal inbound server shared by the demo agents (stdlib only).

The sidecar forwards each mesh call to the agent as:
    POST /invoke
    X-AgentMesh-Capability: <capability>
    <json body>

run_agent wires that contract to a plain Python function:
    handle(capability: str, payload: dict) -> dict
"""

from __future__ import annotations

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Callable


def run_agent(handle: Callable[[str, dict], dict], port: int) -> None:
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):  # noqa: N802 (stdlib naming)
            length = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(length) if length else b""
            try:
                payload = json.loads(raw) if raw else {}
            except json.JSONDecodeError:
                payload = {"_raw": raw.decode("utf-8", "replace")}
            capability = self.headers.get("X-AgentMesh-Capability", "")

            try:
                result = handle(capability, payload)
                body = json.dumps(result).encode("utf-8")
                status = 200
            except Exception as e:  # noqa: BLE001 (surface any error to the mesh)
                body = json.dumps({"error": str(e)}).encode("utf-8")
                status = 500

            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_args):  # silence per-request logging
            pass

    print(f"agent listening on 127.0.0.1:{port}", flush=True)
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
