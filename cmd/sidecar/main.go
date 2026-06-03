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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

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
	)
	flag.Parse()

	if *agentID == "" || *capabilities == "" || *endpoint == "" {
		log.Fatal("--agent-id, --capabilities, and --endpoint are required")
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
