"""Orchestrator (the planner) — capability "interview-guide".

Fans out to the specialist agents through the mesh, then returns the compiled
guide. This is the agent that exercises agent-to-agent calls: it never addresses
any peer directly, only by capability via its sidecar's mesh API.

Input:  {"role": "<job title>"}
Output: {"role", "guide_markdown"}
"""

import os
import sys

# Make the repo's Python client importable without installation.
_REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
sys.path.insert(0, os.path.join(_REPO_ROOT, "clients", "python"))

from agentmesh import Mesh  # noqa: E402

from _serve import run_agent  # noqa: E402

mesh = Mesh()  # talks to this agent's sidecar (AGENTMESH_API or 127.0.0.1:9099)


def handle(_capability: str, payload: dict) -> dict:
    role = payload.get("role", "Software Engineer")

    persona = mesh.invoke("persona", {"role": role})
    questions = mesh.invoke("questions", {"role": role, "persona": persona["persona"]})
    critique = mesh.invoke("critique", {"questions": questions["questions"]})
    guide = mesh.invoke(
        "compile",
        {
            "role": role,
            "persona": persona["persona"],
            "questions": questions["questions"],
            "critique": critique["critique"],
        },
    )
    return guide


if __name__ == "__main__":
    run_agent(handle, 8000)
