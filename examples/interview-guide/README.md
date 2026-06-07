# Interview-Guide demo

A multi-agent demo that exercises the **A2A data plane** and the **Python mesh
client**. Five agents collaborate to produce a markdown interview guide, talking
to each other only by *capability* — never by address.

## The agents

| Agent | Capability | App port | Sidecar (A2A) | Role |
|---|---|---|---|---|
| `persona-1` | `persona` | 8001 | 9101 | builds a candidate persona from a role |
| `questions-1` | `questions` | 8002 | 9102 | designs interview questions |
| `critic-1` | `critique` | 8003 | 9103 | reviews the questions |
| `compiler-1` | `compile` | 8004 | 9104 | assembles the markdown guide |
| `orchestrator-1` | `interview-guide` | 8000 | 9100 | the planner: fans out to the others |

The orchestrator is the only one that makes outbound calls. Its sidecar exposes
the mesh API on `127.0.0.1:9099`; the specialists run with it disabled.

## Flow

```
invoke "interview-guide"
        │
        ▼
  orchestrator ──mesh.invoke("persona")────▶ persona
              ──mesh.invoke("questions")──▶ questions
              ──mesh.invoke("critique")───▶ critic
              ──mesh.invoke("compile")────▶ compiler ──▶ markdown guide
```

Every hop goes orchestrator → its sidecar → (SelectTarget + circuit breaker +
A2A `message/send`) → peer sidecar → peer agent, and back.

## Prerequisites

- Go and Python 3 (the agents and the mesh client are stdlib-only — no `pip install`).
- A running **control plane** with the Agent CRD installed. It needs a Kubernetes
  cluster (e.g. [kind](https://kind.sigs.k8s.io/)):

  ```bash
  # from the repo root, against your cluster
  make install      # install the Agent CRD
  make run          # run the control plane (gRPC :9091, dashboard :8082)
  ```

- Optional: a Jaeger/OTLP collector on `localhost:4317` for traces (the dashboard
  visualizes the mesh regardless).

## Run

```bash
# terminal 1: control plane (see above)
make run

# terminal 2: the agents + sidecars
./examples/interview-guide/run.sh

# terminal 3: trigger it
go run ./cmd/sidecar --data-plane a2a \
  --invoke-capability interview-guide \
  --invoke-payload '{"role":"Backend Engineer"}' --invoke-from demo
```

You'll get back a `guide_markdown` field containing the assembled guide, and the
dashboard (`:8082`) will show the orchestrator fanning out to the four agents.

## Notes

- This uses the **A2A** transport end to end (`--data-plane a2a`). The legacy gRPC
  data plane is still present and can be selected with `--data-plane grpc`.
- The agents use templated (non-LLM) logic on purpose — the point is the mesh
  wiring, not answer quality. Swap `handle(...)` in any agent for a real model
  call to make it intelligent.
