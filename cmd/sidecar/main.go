/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command sidecar runs the AgentMesh agent sidecar. It parses flags, then hands
// off to internal/sidecar. The data-plane transport is pluggable; this wires in
// the gRPC implementation (internal/dataplane/grpcdp).
package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/neontvn/agent-mesh/internal/dataplane"
	"github.com/neontvn/agent-mesh/internal/dataplane/a2adp"
	"github.com/neontvn/agent-mesh/internal/dataplane/grpcdp"
	"github.com/neontvn/agent-mesh/internal/sidecar"
)

func main() {
	var (
		controlPlaneAddr = flag.String("control-plane-addr", "localhost:9091",
			"address of the control plane gRPC server")
		agentID = flag.String("agent-id", "",
			"unique identifier for this agent (required in server mode)")
		capabilities = flag.String("capabilities", "",
			"comma-separated list of capabilities (e.g., search,summarize)")
		endpoint = flag.String("endpoint", "",
			"network endpoint where this agent receives A2A traffic")
		metadataStr = flag.String("metadata", "",
			"comma-separated key=value pairs (e.g., framework=langgraph,version=v1)")
		heartbeatInterval = flag.Duration("heartbeat-interval", 10*time.Second,
			"how often the sidecar sends Heartbeat (must be less than the lease TTL)")
		listenAddr = flag.String("listen-addr", ":9090",
			"address the sidecar's data-plane server listens on")
		invokeCapability = flag.String("invoke-capability", "",
			"if set, run in client mode: call this capability on a peer and exit")
		invokePayload = flag.String("invoke-payload", "",
			"payload string to send when invoking (used only with --invoke-capability)")
		invokeFrom = flag.String("invoke-from", "cli",
			"identifier reported as the caller in ReportInvoke (used with --invoke-capability)")
		invokeInterval = flag.Duration("invoke-interval", 0,
			"if > 0, repeat the invocation at this interval (until Ctrl+C)")
		otlpEndpoint = flag.String("otlp-endpoint", "localhost:4317",
			"OTLP gRPC endpoint to export traces to")
		forwardToURL = flag.String("forward-to-url", "",
			"HTTP endpoint to forward inbound calls to (required in server mode)")
		dataPlane = flag.String("data-plane", "grpc",
			"sidecar data-plane transport: grpc or a2a")
	)
	flag.Parse()

	ctx := context.Background()

	// Client mode: invoke a capability on a peer via the gRPC outbound transport.
	if *invokeCapability != "" {
		out := grpcdp.NewClient()
		defer out.Close()
		if err := sidecar.RunClient(ctx, sidecar.ClientConfig{
			ControlPlaneAddr: *controlPlaneAddr,
			Capability:       *invokeCapability,
			Payload:          *invokePayload,
			From:             *invokeFrom,
			Interval:         *invokeInterval,
			OTLPEndpoint:     *otlpEndpoint,
		}, out); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Server mode requires identity flags AND a forward URL — the sidecar is
	// pure transport, so there must be a local agent to dispatch to.
	if *agentID == "" || *capabilities == "" || *endpoint == "" || *forwardToURL == "" {
		log.Fatal("--agent-id, --capabilities, --endpoint, and --forward-to-url are required (server mode)")
	}

	cfg := sidecar.Config{
		ControlPlaneAddr:  *controlPlaneAddr,
		AgentID:           *agentID,
		Capabilities:      splitAndTrim(*capabilities),
		Endpoint:          *endpoint,
		Metadata:          parseMetadata(*metadataStr),
		HeartbeatInterval: *heartbeatInterval,
		ListenAddr:        *listenAddr,
		OTLPEndpoint:      *otlpEndpoint,
		ForwardToURL:      *forwardToURL,
	}
	var inbound dataplane.Inbound
	switch *dataPlane {
	case "grpc":
		inbound = grpcdp.NewServer()
	case "a2a":
		card := a2adp.BuildCard(cfg.AgentID, "AgentMesh agent "+cfg.AgentID, cfg.Endpoint, "0.1.0", cfg.Capabilities)
		inbound = a2adp.NewServer(card)
	default:
		log.Fatalf("unknown --data-plane %q (want grpc or a2a)", *dataPlane)
	}

	if err := sidecar.Run(ctx, cfg, inbound); err != nil {
		log.Fatal(err)
	}
}

// splitAndTrim splits a comma-separated string and trims each element. Empty
// input returns nil.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// parseMetadata parses comma-separated "key=value" pairs into a map. Malformed
// pairs (no "=") are silently skipped.
func parseMetadata(s string) map[string]string {
	m := map[string]string{}
	if s == "" {
		return m
	}
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		m[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return m
}
