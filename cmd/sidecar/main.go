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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

func main() {
	var (
		controlPlaneAddr = flag.String("control-plane-addr", "localhost:9091",
			"address of the control plane gRPC server")
		agentID = flag.String("agent-id", "",
			"unique identifier for this agent (required)")
		capabilities = flag.String("capabilities", "",
			"comma-separated list of capabilities (e.g., search,summarize) (required)")
		endpoint = flag.String("endpoint", "",
			"network endpoint where this agent receives A2A traffic (required)")
		metadataStr = flag.String("metadata", "",
			"comma-separated key=value pairs (e.g., framework=langgraph,version=v1)")
		heartbeatInterval = flag.Duration("heartbeat-interval", 10*time.Second,
			"how often the sidecar sends Heartbeat (must be less than the lease TTL)")
		listenAddr = flag.String("listen-addr", ":9090",
			"address the sidecar's A2A gRPC server listens on")
		invokeCapability = flag.String("invoke-capability", "",
			"if set, run in client mode: call this capability on a peer and exit")
		invokePayload = flag.String("invoke-payload", "",
			"payload string to send when invoking (used only with --invoke-capability)")
		invokeFrom = flag.String("invoke-from", "cli",
			"identifier reported as the caller in ReportInvoke (used with --invoke-capability)")
	)
	flag.Parse()

	// Client mode: ask the control plane to pick a target for the given
	// capability, dial it directly, invoke, print the response, exit.
	if *invokeCapability != "" {
		runClient(*controlPlaneAddr, *invokeCapability, *invokePayload, *invokeFrom)
		return
	}

	// Server mode requires identity flags.
	if *agentID == "" || *capabilities == "" || *endpoint == "" {
		log.Fatal("--agent-id, --capabilities, and --endpoint are required (server mode)")
	}

	capList := splitAndTrim(*capabilities)
	metadata := parseMetadata(*metadataStr)

	log.Printf("connecting to control plane at %s", *controlPlaneAddr)
	conn, err := grpc.NewClient(
		*controlPlaneAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to dial control plane: %v", err)
	}
	defer conn.Close()

	client := pb.NewControlPlaneClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("registering agent %q with capabilities %v", *agentID, capList)
	resp, err := client.Register(ctx, &pb.RegisterRequest{
		AgentId:      *agentID,
		Capabilities: capList,
		Endpoint:     *endpoint,
		Metadata:     metadata,
	})
	if err != nil {
		log.Fatalf("Register failed: %v", err)
	}

	fmt.Printf("Registered. lease_id=%s ttl=%ds\n", resp.LeaseId, resp.LeaseTtlSeconds)

	// Start the A2A gRPC server so peer sidecars can invoke capabilities
	// on this agent. Runs in a goroutine alongside the heartbeat loop.
	go func() {
		lis, err := net.Listen("tcp", *listenAddr)
		if err != nil {
			log.Fatalf("failed to listen on %s: %v", *listenAddr, err)
		}

		grpcServer := grpc.NewServer()
		pb.RegisterAgentDataPlaneServer(grpcServer, &dataPlaneServer{agentID: *agentID})
		reflection.Register(grpcServer)

		log.Printf("listening for A2A calls on %s", *listenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("A2A server crashed: %v", err)
		}
	}()

	// Wire up signal handling so Ctrl+C exits cleanly instead of leaving
	// the heartbeat goroutine running.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(*heartbeatInterval)
	defer ticker.Stop()

	log.Printf("starting heartbeat loop (every %s)", *heartbeatInterval)
	for {
		select {
		case <-ticker.C:
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := client.Heartbeat(hbCtx, &pb.HeartbeatRequest{
				AgentId: *agentID,
				LeaseId: resp.LeaseId,
				Health:  pb.HealthState_HEALTH_STATE_HEALTHY,
				Load:    0.0,
			})
			hbCancel()
			if err != nil {
				log.Printf("heartbeat failed: %v", err)
			} else {
				log.Printf("heartbeat ok")
			}
		case sig := <-sigCh:
			log.Printf("received signal %s, shutting down", sig)
			return
		}
	}
}

// splitAndTrim splits a comma-separated string and trims whitespace from each
// element. Empty input returns nil.
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

// dataPlaneServer implements the AgentDataPlane gRPC service. For v0 it
// returns a canned response acknowledging the call; the real plug-in
// point to a user-supplied agent application lands later.
type dataPlaneServer struct {
	pb.UnimplementedAgentDataPlaneServer

	// agentID is included in the canned response so we can verify which
	// sidecar handled the call when debugging.
	agentID string
}

// Invoke handles an inbound A2A capability call.
func (d *dataPlaneServer) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	log.Printf("received Invoke: capability=%s payload=%d bytes",
		req.Capability, len(req.Payload))

	return &pb.InvokeResponse{
		Payload: []byte(fmt.Sprintf(
			"hello from %s (cap=%s, payload_bytes=%d)",
			d.agentID, req.Capability, len(req.Payload),
		)),
	}, nil
}

// runClient executes a one-shot capability invocation: it asks the control
// plane to SelectTarget for the capability, dials the chosen agent's
// sidecar directly, calls Invoke, prints the response, and exits.
func runClient(controlPlaneAddr, capability, payload, from string) {
	// Dial the control plane.
	cpConn, err := grpc.NewClient(
		controlPlaneAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("dial control plane: %v", err)
	}
	defer cpConn.Close()

	cpClient := pb.NewControlPlaneClient(cpConn)

	// Ask the control plane to choose a target for this capability.
	selCtx, selCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer selCancel()

	sel, err := cpClient.SelectTarget(selCtx, &pb.SelectTargetRequest{
		Capability: capability,
	})
	if err != nil {
		log.Fatalf("SelectTarget: %v", err)
	}

	target := sel.Agent
	log.Printf("selected target: agent=%s endpoint=%s", target.AgentId, target.Endpoint)

	// Dial the chosen sidecar's A2A endpoint.
	dpConn, err := grpc.NewClient(
		target.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("dial peer sidecar: %v", err)
	}
	defer dpConn.Close()

	dpClient := pb.NewAgentDataPlaneClient(dpConn)

	// Invoke the capability.
	invCtx, invCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer invCancel()

	invokeStart := time.Now()
	resp, err := dpClient.Invoke(invCtx, &pb.InvokeRequest{
		Capability: capability,
		Payload:    []byte(payload),
	})
	durationMs := time.Since(invokeStart).Milliseconds()
	ok := err == nil

	// Report the invocation to the control plane so the live UI can
	// visualize it. Best-effort; failures here do not affect the call.
	repCtx, repCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _ = cpClient.ReportInvoke(repCtx, &pb.ReportInvokeRequest{
		CallerId:   from,
		CalleeId:   target.AgentId,
		Capability: capability,
		DurationMs: durationMs,
		Ok:         ok,
	})
	repCancel()

	if err != nil {
		log.Fatalf("Invoke: %v", err)
	}

	fmt.Printf("response from %s: %s\n", target.AgentId, string(resp.Payload))
}
