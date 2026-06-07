# AgentMesh Python client

A minimal client (stdlib only, no dependencies) for agents that run behind an
AgentMesh sidecar.

## The two directions

An agent talks to the mesh only through its **local sidecar**. The sidecar does
all the protocol work (discovery, routing, circuit breaking, A2A, tracing).

### Inbound — receiving work (no client needed)

The sidecar forwards each incoming call to your agent as a plain HTTP request:

```
POST <forward-url>            # the --forward-to-url the sidecar was started with
Content-Type: application/json
X-AgentMesh-Capability: <capability>
X-AgentMesh-Meta-<k>: <v>     # one header per metadata entry
<body>                        # the input, passed through unchanged
```

Respond `200 OK` with the result as the body (any non-200 is treated as a
failure). Any web framework works:

```python
from fastapi import FastAPI, Request

app = FastAPI()

@app.post("/invoke")
async def invoke(req: Request):
    capability = req.headers.get("X-AgentMesh-Capability")
    payload = await req.json()
    # ... do the work ...
    return {"result": "..."}
```

### Outbound — calling another agent

Use the client. It posts to the sidecar's loopback mesh API; the sidecar picks a
healthy target, calls it, and returns the result.

```python
from agentmesh import Mesh

mesh = Mesh()  # http://127.0.0.1:9099 by default; set AGENTMESH_API to override

# A planner fanning out to other agents:
hits = mesh.invoke("search", {"query": "service mesh for agents"})
summary = mesh.invoke("summarize", {"text": hits["text"]})
```

`invoke(capability, input, metadata=None)` returns the target's result parsed as
JSON when possible (else raw text), and raises `MeshError` on failure.

## Configuration

| Setting | Source | Default |
|---|---|---|
| Mesh API URL | `AGENTMESH_API` env or `Mesh(base_url=...)` | `http://127.0.0.1:9099` |

The sidecar exposes the mesh API on the address given by its `--mesh-api-addr`
flag (default `127.0.0.1:9099`).
