package sidecar

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/neontvn/agent-mesh/internal/dataplane"
	"github.com/neontvn/agent-mesh/internal/tracing"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// Run starts the sidecar in server mode: initialize tracing, register with the
// control plane, serve the inbound data plane (dispatching to the local agent),
// and heartbeat until interrupted. It blocks until a signal or ctx cancellation.
func Run(ctx context.Context, cfg Config, inbound dataplane.Inbound) error {
	shutdownTracer, err := tracing.Init(ctx, "sidecar-"+cfg.AgentID, cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("tracing init: %w", err)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(sctx)
	}()

	log.Printf("connecting to control plane at %s", cfg.ControlPlaneAddr)
	conn, err := grpc.NewClient(
		cfg.ControlPlaneAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("dial control plane: %w", err)
	}
	defer conn.Close()
	client := pb.NewControlPlaneClient(conn)

	regCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	log.Printf("registering agent %q with capabilities %v", cfg.AgentID, cfg.Capabilities)
	resp, err := client.Register(regCtx, &pb.RegisterRequest{
		AgentId:      cfg.AgentID,
		Capabilities: cfg.Capabilities,
		Endpoint:     cfg.Endpoint,
		Metadata:     cfg.Metadata,
	})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	fmt.Printf("Registered. lease_id=%s ttl=%ds\n", resp.LeaseId, resp.LeaseTtlSeconds)

	// Start the inbound data plane so peer sidecars can call this agent.
	agent := newHTTPLocalAgent(cfg.ForwardToURL, &http.Client{Timeout: 30 * time.Second})
	srvCtx, srvCancel := context.WithCancel(ctx)
	defer srvCancel()
	go func() {
		if err := inbound.Serve(srvCtx, cfg.ListenAddr, agent); err != nil {
			log.Printf("inbound data plane stopped: %v", err)
		}
	}()

	// Heartbeat loop with clean shutdown on signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	log.Printf("starting heartbeat loop (every %s)", cfg.HeartbeatInterval)
	for {
		select {
		case <-ticker.C:
			hbCtx, hbCancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := client.Heartbeat(hbCtx, &pb.HeartbeatRequest{
				AgentId: cfg.AgentID,
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
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
