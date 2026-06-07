"""AgentMesh Python client.

A thin helper for agents to call other agents through their local sidecar's
mesh API. The sidecar handles discovery, routing, circuit breaking, A2A
transport, and tracing; this client just speaks the loopback contract.

Inbound (receiving work) needs no client: the sidecar forwards each call to the
agent as `POST <forward-url>` with header `X-AgentMesh-Capability` and the input
as the body. Return an HTTP 200 with the result body. See the README.

Outbound (calling another agent):

    from agentmesh import Mesh

    mesh = Mesh()  # defaults to http://127.0.0.1:9099 (AGENTMESH_API env overrides)
    summary = mesh.invoke("summarize", {"text": "..."})
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any, Mapping, Optional

__all__ = ["Mesh", "MeshError"]

DEFAULT_API = "http://127.0.0.1:9099"


class MeshError(Exception):
    """Raised when a mesh invocation fails."""


class Mesh:
    """Client for the sidecar's outbound mesh API."""

    def __init__(self, base_url: Optional[str] = None, timeout: float = 30.0) -> None:
        self.base_url = (base_url or os.environ.get("AGENTMESH_API", DEFAULT_API)).rstrip("/")
        self.timeout = timeout

    def invoke(
        self,
        capability: str,
        input: Any,
        metadata: Optional[Mapping[str, str]] = None,
    ) -> Any:
        """Call an agent advertising `capability` with `input`.

        `input` is any JSON-serializable value, passed to the target unchanged.
        Returns the target's result parsed as JSON when possible, else raw text.
        Raises MeshError on failure.
        """
        body = json.dumps(
            {"capability": capability, "input": input, "metadata": dict(metadata or {})}
        ).encode("utf-8")

        req = urllib.request.Request(
            f"{self.base_url}/mesh/invoke",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as e:
            detail = e.read().decode("utf-8", "replace")
            raise MeshError(f"mesh invoke {capability!r} failed: HTTP {e.code}: {detail}") from e
        except urllib.error.URLError as e:
            raise MeshError(f"mesh invoke {capability!r} failed: {e.reason}") from e

        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return raw.decode("utf-8", "replace")
