// Package dataplane defines the transport-agnostic contract for sidecar-to-
// sidecar (agent-to-agent) communication. Concrete transports — gRPC today,
// A2A next — live in subpackages and implement these interfaces, so the sidecar
// orchestration depends only on the abstraction and a transport can be swapped
// by wiring a different implementation in cmd/sidecar.
package dataplane

import "context"

// LocalAgent is the agent application this sidecar fronts. An inbound transport
// dispatches each received call to the LocalAgent. It is independent of how
// peers reach the sidecar.
type LocalAgent interface {
	// Invoke runs a capability on the local agent and returns its result.
	Invoke(ctx context.Context, capability string, payload []byte, meta map[string]string) ([]byte, error)
}

// Inbound is a transport that receives peer calls and dispatches them to the
// LocalAgent. Serve blocks until ctx is canceled or the transport stops.
type Inbound interface {
	Serve(ctx context.Context, listenAddr string, agent LocalAgent) error
}

// Outbound is a transport that invokes a capability on a specific peer
// endpoint. Implementations should be safe for concurrent use and may pool
// connections; Close releases any such resources.
type Outbound interface {
	Invoke(ctx context.Context, endpoint, capability string, payload []byte, meta map[string]string) ([]byte, error)
	Close() error
}
