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

	"github.com/neontvn/agent-mesh/internal/circuit"
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
		invokeInterval = flag.Duration("invoke-interval", 0,
			"if > 0, repeat the invocation at this interval (until Ctrl+C)")
	)
	flag.Parse()

	// Client mode: ask the control plane to pick a target for the given
	// capability, dial it directly, invoke, print the response, exit.
	if *invokeCapability != "" {
		runClient(*controlPlaneAddr, *invokeCapability, *invokePayload, *invokeFrom, *invokeInterval)
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

// runClient runs the sidecar in outbound-client mode. It asks the control
// plane for a target advertising the given capability, dials the chosen
// peer, calls Invoke, and reports the outcome to the control plane.
//
// A per-peer circuit breaker (3 consecutive failures -> opens for 15s)
// protects against repeatedly dispatching to a dead peer. When a peer's
// circuit is open, runClient re-asks SelectTarget to get a different
// candidate, up to maxAttempts per invocation.
//
// If interval > 0, the invocation repeats at that cadence until Ctrl+C;
// breaker state and peer connections persist across iterations.
func runClient(controlPlaneAddr, capability, payload, from string, interval time.Duration) {
	cpConn, err := grpc.NewClient(
		controlPlaneAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("dial control plane: %v", err)
	}
	defer cpConn.Close()

	cpClient := pb.NewControlPlaneClient(cpConn)

	// Per-peer breaker: open after 3 consecutive failures; stay open for 15s.
	breaker := circuit.New(3, 15*time.Second)

	// Persistent connection pool keyed by endpoint, so we don't redial on
	// every invocation in loop mode.
	peerConns := map[string]*grpc.ClientConn{}
	defer func() {
		for _, c := range peerConns {
			_ = c.Close()
		}
	}()

	invoke := func() {
		const maxAttempts = 5
		for attempt := 0; attempt < maxAttempts; attempt++ {
			selCtx, selCancel := context.WithTimeout(context.Background(), 5*time.Second)
			sel, selErr := cpClient.SelectTarget(selCtx, &pb.SelectTargetRequest{
				Capability: capability,
			})
			selCancel()
			if selErr != nil {
				log.Printf("SelectTarget: %v", selErr)
				return
			}
			target := sel.Agent

			if breaker.IsOpen(target.AgentId) {
				log.Printf("circuit OPEN for %s; asking for another peer", target.AgentId)
				continue
			}

			peerConn, dialErr := poolDial(peerConns, target.Endpoint)
			if dialErr != nil {
				log.Printf("dial %s (%s) failed: %v", target.AgentId, target.Endpoint, dialErr)
				onFailure(cpClient, breaker, from, target.AgentId, capability, 0)
				continue
			}

			dpClient := pb.NewAgentDataPlaneClient(peerConn)
			invCtx, invCancel := context.WithTimeout(context.Background(), 10*time.Second)
			start := time.Now()
			resp, invErr := dpClient.Invoke(invCtx, &pb.InvokeRequest{
				Capability: capability,
				Payload:    []byte(payload),
			})
			invCancel()
			durationMs := time.Since(start).Milliseconds()

			if invErr != nil {
				log.Printf("invoke %s failed: %v", target.AgentId, invErr)
				onFailure(cpClient, breaker, from, target.AgentId, capability, durationMs)
				continue
			}

			breaker.RecordSuccess(target.AgentId)
			reportInvoke(cpClient, from, target.AgentId, capability, durationMs, true)
			fmt.Printf("response from %s: %s\n", target.AgentId, string(resp.Payload))
			return
		}
		log.Printf("all %d attempts exhausted", 5)
	}

	invoke()

	if interval <= 0 {
		return
	}

	log.Printf("starting invoke loop (every %s, Ctrl+C to stop)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-ticker.C:
			invoke()
		case sig := <-sigCh:
			log.Printf("received signal %s, shutting down", sig)
			return
		}
	}
}

// onFailure records the failure to the breaker (logging if it caused the
// circuit to open) and fires a ReportInvoke with ok=false.
func onFailure(cp pb.ControlPlaneClient, b *circuit.Breaker, from, peer, capability string, durationMs int64) {
	if opened := b.RecordFailure(peer); opened {
		log.Printf("CIRCUIT OPENED for %s (cooldown 15s)", peer)
	}
	reportInvoke(cp, from, peer, capability, durationMs, false)
}

// reportInvoke fires the ReportInvoke RPC; best-effort, errors ignored.
func reportInvoke(cp pb.ControlPlaneClient, from, peer, capability string, durationMs int64, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = cp.ReportInvoke(ctx, &pb.ReportInvokeRequest{
		CallerId:   from,
		CalleeId:   peer,
		Capability: capability,
		DurationMs: durationMs,
		Ok:         ok,
	})
}

// poolDial returns the existing gRPC connection for the endpoint, or
// creates and caches one. grpc.NewClient is lazy: errors surface on the
// first RPC call, not on the dial itself.
func poolDial(pool map[string]*grpc.ClientConn, endpoint string) (*grpc.ClientConn, error) {
	if conn, ok := pool[endpoint]; ok {
		return conn, nil
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	pool[endpoint] = conn
	return conn, nil
}
