"""
Thin Python client for invoking capabilities through AgentMesh.

Wraps the generated gRPC stubs so agents (notably the planner) can speak
to the mesh without dealing with protobuf directly. Each invoke():

  1. Calls SelectTarget on the control plane to pick a healthy agent
     advertising the requested capability.
  2. Reuses (or opens) a gRPC channel to that agent's sidecar.
  3. Calls Invoke with the JSON-encoded payload.
  4. Reports the outcome via ReportInvoke (best-effort) so the live UI
     and tracing systems can observe the call.
"""
from __future__ import annotations

import json
import logging
import time
from typing import Any, Dict

import grpc

# Generated stubs live in ./agentmesh/v1/ relative to this file.
from agentmesh.v1 import control_plane_pb2, control_plane_pb2_grpc
from agentmesh.v1 import data_plane_pb2, data_plane_pb2_grpc

log = logging.getLogger("agentmesh_client")


class MeshError(Exception):
    """Raised when an invocation through the mesh fails."""


class MeshClient:
    def __init__(self, control_plane_addr: str = "localhost:9091") -> None:
        self.control_plane_addr = control_plane_addr
        self._cp_channel = grpc.insecure_channel(control_plane_addr)
        self._cp_stub = control_plane_pb2_grpc.ControlPlaneStub(self._cp_channel)
        self._peer_channels: Dict[str, grpc.Channel] = {}
        self._peer_stubs: Dict[str, data_plane_pb2_grpc.AgentDataPlaneStub] = {}

    def close(self) -> None:
        for ch in self._peer_channels.values():
            ch.close()
        self._cp_channel.close()

    # ---------- public API ----------

    def select_target(self, capability: str) -> tuple[str, str]:
        """Returns (agent_id, endpoint) for a healthy agent advertising capability."""
        req = control_plane_pb2.SelectTargetRequest(capability=capability)
        try:
            resp = self._cp_stub.SelectTarget(req, timeout=5)
        except grpc.RpcError as exc:
            raise MeshError(f"SelectTarget({capability}): {exc.details()}") from exc
        return resp.agent.agent_id, resp.agent.endpoint

    def invoke(
        self,
        capability: str,
        payload: Any,
        caller_id: str = "planner",
    ) -> Any:
        """Invoke a capability and return the JSON-decoded response.

        payload may be any JSON-serializable Python object; it is encoded as
        UTF-8 JSON bytes on the wire. If the agent returns non-JSON bytes,
        the raw bytes are returned.
        """
        body = (
            json.dumps(payload).encode("utf-8")
            if not isinstance(payload, (bytes, bytearray))
            else bytes(payload)
        )

        agent_id, endpoint = self.select_target(capability)
        log.info("invoke %s -> %s @ %s", capability, agent_id, endpoint)

        stub = self._stub_for(endpoint)

        started = time.time()
        ok = True
        err: Exception | None = None
        inv_resp = None
        try:
            inv_req = data_plane_pb2.InvokeRequest(capability=capability, payload=body)
            inv_resp = stub.Invoke(inv_req, timeout=90)
        except grpc.RpcError as exc:
            ok = False
            err = exc
        duration_ms = int((time.time() - started) * 1000)

        # Report the outcome so the live UI can visualize it.
        try:
            self._cp_stub.ReportInvoke(
                control_plane_pb2.ReportInvokeRequest(
                    caller_id=caller_id,
                    callee_id=agent_id,
                    capability=capability,
                    duration_ms=duration_ms,
                    ok=ok,
                ),
                timeout=5,
            )
        except grpc.RpcError as report_err:
            log.warning("ReportInvoke failed: %s", report_err)

        if not ok:
            detail = err.details() if isinstance(err, grpc.RpcError) else str(err)
            raise MeshError(f"Invoke({capability} -> {agent_id}): {detail}") from err

        try:
            return json.loads(inv_resp.payload)
        except json.JSONDecodeError:
            # If the agent returned raw bytes (not JSON), surface them as-is.
            return inv_resp.payload

    # ---------- internals ----------

    def _stub_for(self, endpoint: str) -> data_plane_pb2_grpc.AgentDataPlaneStub:
        if endpoint not in self._peer_stubs:
            self._peer_channels[endpoint] = grpc.insecure_channel(endpoint)
            self._peer_stubs[endpoint] = data_plane_pb2_grpc.AgentDataPlaneStub(
                self._peer_channels[endpoint]
            )
        return self._peer_stubs[endpoint]
