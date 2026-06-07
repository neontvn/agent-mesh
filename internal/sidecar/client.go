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

	"github.com/neontvn/agent-mesh/internal/dataplane"
	"github.com/neontvn/agent-mesh/internal/tracing"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// RunClient runs the sidecar in outbound-client mode using the given Outbound
// transport. It is a thin CLI wrapper around Caller: invoke once, then repeat at
// cfg.Interval if set. Target selection, circuit breaking, and reporting all
// live in Caller, shared with the in-process mesh API.
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

	caller := NewCaller(pb.NewControlPlaneClient(cpConn), out, cfg.From)

	invoke := func() {
		res, err := caller.Invoke(ctx, cfg.Capability, []byte(cfg.Payload), nil)
		if err != nil {
			log.Printf("invoke failed: %v", err)
			return
		}
		fmt.Printf("response: %s\n", string(res))
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
