// Package sidecar contains the agent sidecar: lifecycle (register, heartbeat,
// shutdown), the outbound invocation orchestration (target selection, circuit
// breaking, outcome reporting), and the bridge to the local agent application.
// It depends on the transport only through the dataplane interfaces, so the
// underlying transport (gRPC, A2A) is pluggable.
package sidecar

import "time"

// Config holds the settings for server mode, populated from CLI flags.
type Config struct {
	ControlPlaneAddr  string
	AgentID           string
	Capabilities      []string
	Endpoint          string
	Metadata          map[string]string
	HeartbeatInterval time.Duration
	ListenAddr        string
	OTLPEndpoint      string
	ForwardToURL      string

	// AgentCardJSON is the agent's A2A AgentCard as canonical JSON, sent to the
	// control plane at registration. Empty when not running the A2A data plane.
	AgentCardJSON string
}

// ClientConfig holds the settings for outbound client mode (one-shot or loop).
type ClientConfig struct {
	ControlPlaneAddr string
	Capability       string
	Payload          string
	From             string
	Interval         time.Duration
	OTLPEndpoint     string
}
