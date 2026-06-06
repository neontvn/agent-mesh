package sidecar

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/neontvn/agent-mesh/internal/circuit"
	"github.com/neontvn/agent-mesh/internal/dataplane"
	"github.com/neontvn/agent-mesh/internal/tracing"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// RunClient runs the sidecar in outbound-client mode using the given Outbound
// transport. For each invocation it asks the control plane to SelectTarget for
// the capability, dispatches through a per-peer circuit breaker (open after 3
// consecutive failures, 15s cooldown), and reports the outcome with
// ReportInvoke. If cfg.Interval > 0 it repeats until interrupted.
//
// Target selection, breaking, and reporting are transport-agnostic: they wrap
// any dataplane.Outbound, so the same orchestration serves gRPC or A2A.
func RunClient(ctx context.Context, cfg ClientConfig, out dataplane.Outbound) error {
	shutdownTracer, err := tracing.Init(ctx, "sidecar-cli", cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("tracing init: %w", err)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracer(sctx)
	}()

	cpConn, err := grpc.NewClient(
		cfg.ControlPlaneAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("dial control plane: %w", err)
	}
	defer cpConn.Close()
	cp := pb.NewControlPlaneClient(cpConn)

	breaker := circuit.New(3, 15*time.Second)

	invoke := func() {
		const maxAttempts = 5
		for attempt := 0; attempt < maxAttempts; attempt++ {
			selCtx, selCancel := context.WithTimeout(ctx, 5*time.Second)
			sel, selErr := cp.SelectTarget(selCtx, &pb.SelectTargetRequest{Capability: cfg.Capability})
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

			invCtx, invCancel := context.WithTimeout(ctx, 10*time.Second)
			start := time.Now()
			payload, invErr := out.Invoke(invCtx, target.Endpoint, cfg.Capability, []byte(cfg.Payload), nil)
			invCancel()
			durationMs := time.Since(start).Milliseconds()

			if invErr != nil {
				log.Printf("invoke %s failed: %v", target.AgentId, invErr)
				if opened := breaker.RecordFailure(target.AgentId); opened {
					log.Printf("CIRCUIT OPENED for %s (cooldown 15s)", target.AgentId)
				}
				reportInvoke(ctx, cp, cfg.From, target.AgentId, cfg.Capability, durationMs, false)
				continue
			}

			breaker.RecordSuccess(target.AgentId)
			reportInvoke(ctx, cp, cfg.From, target.AgentId, cfg.Capability, durationMs, true)
			fmt.Printf("response from %s: %s\n", target.AgentId, string(payload))
			return
		}
		log.Printf("all %d attempts exhausted", 5)
	}

	invoke()
	if cfg.Interval <= 0 {
		return nil
	}

	log.Printf("starting invoke loop (every %s, Ctrl+C to stop)", cfg.Interval)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case <-ticker.C:
			invoke()
		case sig := <-sigCh:
			log.Printf("received signal %s, shutting down", sig)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// reportInvoke fires the ReportInvoke RPC; best-effort, errors ignored.
func reportInvoke(ctx context.Context, cp pb.ControlPlaneClient, from, peer, capability string, durationMs int64, ok bool) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = cp.ReportInvoke(rctx, &pb.ReportInvokeRequest{
		CallerId:   from,
		CalleeId:   peer,
		Capability: capability,
		DurationMs: durationMs,
		Ok:         ok,
	})
}
