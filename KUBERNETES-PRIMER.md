# Kubernetes Primer — Key Concepts You've Touched

This document is a reference, not a tutorial. It defines the important names, types, and functions you have encountered while bootstrapping AgentMesh so far — what each one is, why it matters, and where it appeared in your code. Come back to it whenever a term feels fuzzy.

---

## 1. Cluster fundamentals

### Cluster
A group of machines that Kubernetes manages as a single system. Your `kind` cluster (`agentmesh-dev`) is a one-machine cluster running inside a Docker container.

### Node
A single machine in the cluster. In `kind`, your only node is a Docker container that runs both the control plane *and* your workloads. In production, nodes are usually VMs or physical servers.

### Control plane
The "brain" of the cluster. It stores cluster state, exposes the Kubernetes API, schedules workloads, and runs built-in controllers. It is itself a set of processes (api-server, etcd, scheduler, controller-manager) running on one or more nodes.

### kube-apiserver
The HTTP front door to the cluster. Every action — `kubectl` commands, controller reads/writes, webhooks — flows through it. It is the *only* component that talks directly to etcd; everything else talks to the API server.

### etcd
The cluster's strongly-consistent database. Every Kubernetes object — every Pod, Service, CRD instance, your `Agent` records — is stored here. Replicated and uses the Raft consensus algorithm internally.

### kubelet
The agent that runs on every node. It watches the API server for Pods assigned to its node and ensures the right containers are running locally.

---

## 2. The Kubernetes API model

Every Kubernetes object — built-in or custom — follows the same shape, which is why a CRD instance looks structurally identical to a Pod.

### apiVersion
The combined **group + version** identifier for a resource type. Examples:
- `v1` (the core group, no prefix) — used by Pod, Service, ConfigMap
- `apps/v1` — Deployment, StatefulSet
- `agentmesh.agentmesh.io/v1` — your Agent

### kind
The resource type name in PascalCase. `Pod`, `Service`, `Deployment`, `Agent`, `AgentList`.

### metadata
A standard object header that every Kubernetes resource has. Includes `name`, `namespace`, `labels`, `annotations`, `uid`, `resourceVersion`, `creationTimestamp`, and more. In Go, this is the embedded `metav1.ObjectMeta` field.

### spec
The **desired state** of the resource — what *you* (the user) want to be true. The controller's job is to make the real world match this.

### status
The **observed state** of the resource — what the controller has actually achieved or measured. Users do not write here; controllers do.

### GVK (Group / Version / Kind)
The three-part identifier that uniquely names a resource type. For your Agent: Group = `agentmesh.agentmesh.io`, Version = `v1`, Kind = `Agent`.

### NamespacedName
A `{namespace, name}` pair that uniquely identifies a single resource instance within a cluster. You receive one of these in every `Reconcile()` call as `req.NamespacedName`, and you use it to fetch the actual object.

---

## 3. Custom Resources — what you defined in `agent_types.go`

### Custom Resource Definition (CRD)
The mechanism Kubernetes provides for extending its API with new resource types. When you install a CRD, the kube-apiserver starts serving a brand-new REST endpoint for that type with storage, validation, and watch support included.

### AgentSpec
The Go struct that defines the **desired state** of your Agent. Fields you defined:
- `Capabilities []string` — what this agent can do
- `Endpoint string` — where to reach it
- `Metadata map[string]string` — free-form labels

### AgentStatus
The Go struct that defines the **observed state** of your Agent. Fields:
- `Health string` — current health
- `LastHeartbeat *metav1.Time` — when the agent last checked in
- `Conditions []metav1.Condition` — structured condition entries (the standard K8s pattern for richer status)

### Agent
The Go struct that wraps `TypeMeta` (apiVersion + kind), `ObjectMeta` (name, namespace, etc.), `AgentSpec`, and `AgentStatus`. This is what an Agent object *is* in Go.

### AgentList
A wrapper containing `[]Agent`. Kubernetes requires this paired list type for every CRD — it's what gets returned when you list multiple Agents (e.g., `kubectl get agents`).

### `metav1.Time`
A wrapper around Go's `time.Time` that knows how to (de)serialize to/from the JSON timestamp format Kubernetes expects.

### `metav1.Condition`
The standard K8s status sub-object for typed, structured conditions like "Available: true", "Progressing: false". Used universally across the Kubernetes ecosystem.

### `SchemeBuilder`
A registry of all the Go types in your API group. In the `init()` at the bottom of `agent_types.go`, you call `SchemeBuilder.Register(&Agent{}, &AgentList{})` to make these types known to the controller-runtime client. Without this, the client wouldn't know how to serialize or deserialize your custom type.

---

## 4. Kubebuilder markers — the comments that aren't really comments

Special `// +...` comments above Go types and fields that tell `controller-gen` (the code generator) how to produce CRD YAML, RBAC manifests, and webhook configs.

### `// +kubebuilder:object:root=true`
"This struct is a top-level Kubernetes object" (i.e., `Agent` and `AgentList`, not `AgentSpec`). Tells the generator to produce a `DeepCopyObject()` method for it.

### `// +kubebuilder:subresource:status`
"This object has a `status` subresource." Allows the API server to handle updates to `status` separately from `spec`, which is the K8s convention.

### `// +listType=map` and `// +listMapKey=type`
Applied to an *array* field (specifically `Conditions []metav1.Condition`). They tell the API server that this list is map-keyed by the `type` field, so updates to one condition do not blow away the others. Applying these to a non-array field is what caused your earlier build error.

### `// +optional` / `// +required`
Says whether the field must be present on a resource. Affects validation and OpenAPI schema generation.

---

## 5. The controller — what you wrote in `agent_controller.go`

### Controller
A program that watches the API server for changes to a resource type and reconciles the actual state of the world to match the spec.

### Operator
A controller plus the CRDs it manages, packaged as a single deployable thing. Your AgentMesh control plane *is* an operator.

### Manager (controller-runtime)
The Go runtime that hosts your controller. It owns the connection to the API server, runs the event loop, handles leader election (so only one replica is active in a multi-replica deployment), and provides a cached client that you use inside `Reconcile()`. Set up in `main.go`.

### `AgentReconciler`
Your reconciler struct. Embeds `client.Client` (so it can read and write Kubernetes objects) and `*runtime.Scheme` (so it knows your custom types).

### `Reconcile()`
The function the manager calls every time something interesting happens to an Agent — creation, update, deletion, or just a periodic resync. Signature:

```go
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
```

It receives a `req` identifying which Agent changed, reads the current state, decides what to do, and returns. The reconciler is **idempotent**: it should be safe to call any number of times with the same input.

### `ctx context.Context`
Go's standard mechanism for cancellation, deadlines, and request-scoped values. You pass it through to every blocking call (including K8s reads) so things terminate cleanly when the controller shuts down.

### `ctrl.Request`
A lightweight struct containing only the `NamespacedName` of the resource that changed. It does *not* carry the full object — you fetch that yourself with `r.Get(ctx, req.NamespacedName, &agent)`.

### `ctrl.Result`
Tells the manager how to schedule the next reconcile. The most common returns:
- `ctrl.Result{}, nil` — done; only reconcile again when something changes
- `ctrl.Result{RequeueAfter: 30 * time.Second}, nil` — reconcile again in 30 seconds even if nothing changed
- `ctrl.Result{}, err` — error; the manager will back off and retry

### `r.Get(ctx, req.NamespacedName, &agent)`
The cached client read that fetches the current Agent object from the API server. The "cached" part is important: under the hood, `controller-runtime` keeps an in-memory mirror of the API server state via an informer, so this is a local read, not a network hop.

### `log.FromContext(ctx)`
Returns a structured logger pre-populated with context about the current reconcile (resource name, controller name, request UID). Everything you log through it ends up in your `make run` output with that context attached.

### `apierrors.IsNotFound(err)`
Tells you whether the error from `r.Get` means "this object was deleted." It's a normal, expected condition during reconciliation — when a user runs `kubectl delete agent foo`, the next reconcile finds the object gone, and you handle the deletion case.

---

## 6. The generated machinery — what `make` targets do

### `make generate`
Runs `controller-gen object:headerFile=...` to regenerate `zz_generated.deepcopy.go`. Every Kubernetes object needs `DeepCopy()` methods (so updates can be serialized safely). You never write these by hand; they're produced from your struct definitions.

### `make manifests`
Runs `controller-gen rbac crd webhook ...` to regenerate the YAML files in `config/`. Reads your kubebuilder markers and produces:
- CRD YAML in `config/crd/bases/`
- RBAC YAML in `config/rbac/`
- Webhook config in `config/webhook/` (when applicable)

### `make install`
Runs `kubectl apply` on the generated CRD YAML. After this, the cluster's API server knows about `agents.agentmesh.agentmesh.io` and you can `kubectl get agents`.

### `make run`
Compiles your controller and runs it locally, pointed at whatever cluster your kubeconfig is currently aimed at. This is the "fast iteration" mode — no Docker build, no Pod, just a Go process talking to the cluster. The deployed-as-a-Pod equivalent comes later.

### `controller-gen`
The code generation tool that powers `make generate` and `make manifests`. It scans your Go files, looks for kubebuilder markers, and emits the appropriate Go code or YAML.

### `zz_generated.deepcopy.go`
The auto-generated file containing `DeepCopy()`, `DeepCopyInto()`, and `DeepCopyObject()` methods for every type that has `// +kubebuilder:object:root=true` or otherwise needs them. The `zz_` prefix is convention so it sorts to the bottom of the directory and you remember not to edit it by hand.

---

## 7. kubectl — the commands you've run

### `kubectl apply -f <file>`
"Make the cluster look like this YAML." If the object doesn't exist, it's created. If it exists, it's updated. Declarative — repeat-safe.

### `kubectl get <resource>`
Lists resources. `kubectl get agents` returns every Agent in the current namespace.

### `kubectl describe <resource> <name>`
Shows the full object plus recent events. Far more verbose than `get`; useful when something isn't behaving and you want context.

### `kubectl edit <resource> <name>`
Opens the live object in `$EDITOR`. When you save, your changes are applied to the cluster. Triggers a reconcile.

### `kubectl delete <resource> <name>`
Removes the object. Triggers a final reconcile in which `r.Get` returns a `NotFound` error — your reconciler's "Agent deleted" branch.

### `kubectl get crds`
Lists the Custom Resource Definitions installed in the cluster. After `make install`, `agents.agentmesh.agentmesh.io` should appear here.

### `kubectl cluster-info`
Shows the API server's address. Useful when something can't connect and you want to confirm `kubectl` is talking to the cluster you think it is.

---

## 8. The end-to-end flow

When you ran `kubectl apply -f config/samples/research-agent-1.yaml`:

1. `kubectl` POSTed the YAML to the kube-apiserver as JSON.
2. The API server validated it against the CRD schema, then stored it in etcd.
3. etcd emitted a change event.
4. The controller-runtime informer in your controller manager observed the event.
5. The manager invoked `Reconcile()` on your `AgentReconciler`, passing the namespaced name as `req`.
6. Your `Reconcile()` called `r.Get(ctx, req.NamespacedName, &agent)` and the cached client returned the object.
7. Your `logger.Info(...)` line printed in `make run`'s output.
8. `Reconcile()` returned `ctrl.Result{}, nil`. The manager filed it away and waited for the next event.

Every subsequent `kubectl edit`, `kubectl delete`, or `kubectl apply` triggers the same loop. That is the operator pattern.

---

## 9. Vocabulary cheat sheet

| Term | What it is |
|---|---|
| Cluster | The set of machines K8s manages |
| Node | One machine in the cluster |
| Control plane | The brain (api-server, etcd, scheduler, controller-manager) |
| etcd | The cluster's database |
| kube-apiserver | The HTTP front door |
| kubelet | Per-node agent that runs containers |
| Pod | One or more containers running together |
| GVK | Group / Version / Kind |
| apiVersion | Group + Version, as a string |
| spec | Desired state |
| status | Observed state |
| metadata | Standard object header (name, namespace, labels, ...) |
| CRD | Custom Resource Definition — a new type you add |
| Custom Resource (CR) | An instance of a CRD (e.g., `research-agent-1`) |
| Operator | A controller plus the CRDs it manages |
| Controller | The watch-and-reconcile loop |
| Manager | The controller-runtime host process |
| Reconcile() | The function called on every change |
| ctrl.Request | A namespaced-name handle for the changed resource |
| ctrl.Result | Tells the manager when to call Reconcile again |
| ctx | Context for cancellation and deadlines |
| r.Get | Cached read of an object from the API server |
| log.FromContext | Structured logger pre-populated with reconcile context |
| apierrors.IsNotFound | Error check for "this was deleted" |
| SchemeBuilder | Registry of Go types for this API group |
| kubebuilder markers | `// +...` comments that drive code/YAML generation |
| controller-gen | Tool that reads markers and produces code/YAML |
| RBAC | ServiceAccount + Role + RoleBinding for permissions |
| kubectl | CLI that talks to the API server |
| kubebuilder | Scaffolding tool for operators in Go |
| controller-runtime | Go library that implements the operator machinery |
| kind | Kubernetes-in-Docker for local development |

---

## How to use this document

Treat it as a glossary you return to. Whenever a name in your code feels opaque — `ctrl.Request`, `SchemeBuilder`, a kubebuilder marker — find it here and re-anchor. The names recur constantly in Kubernetes work, so each one you internalize pays back many times over.
