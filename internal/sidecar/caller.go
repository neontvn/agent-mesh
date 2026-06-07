package sidecar

import (
	"context"
	"fmt"
	"time"

	"github.com/neontvn/agent-mesh/internal/circuit"
	"github.com/neontvn/agent-mesh/internal/dataplane"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// Caller performs outbound capability invocations. For each call it asks the
// control plane to select a target, dispatches through a per-peer circuit
// breaker, reports the outcome, and retries on a different peer when one is
// unavailable. It is transport-agnostic (via dataplane.Outbound) and is reused
// by both CLI client mode and the in-process mesh API endpoint.
type Caller struct {
	cp      pb.ControlPlaneClient
	out     dataplane.Outbound
	breaker *circuit.Breaker
	from    string
}

// NewCaller builds a Caller. from identifies this caller in ReportInvoke.
func NewCaller(cp pb.ControlPlaneClient, out dataplane.Outbound, from string) *Caller {
	return &Caller{
		cp:      cp,
		out:     out,
		breaker: circuit.New(3, 15*time.Second),
		from:    from,
	}
}

// Invoke selects a healthy peer advertising capability, dispatches input to it,
// and returns the result. It tries up to maxAttempts peers, skipping any whose
// circuit is open, and reports each attempt to the control plane.
func (c *Caller) Invoke(ctx context.Context, capability string, input []byte, meta map[string]string) ([]byte, error) {
	const maxAttempts = 5
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		selCtx, selCancel := context.WithTimeout(ctx, 5*time.Second)
		sel, err := c.cp.SelectTarget(selCtx, &pb.SelectTargetRequest{Capability: capability})
		selCancel()
		if err != nil {
			return nil, fmt.Errorf("select target for %q: %w", capability, err)
		}
		target := sel.Agent

		if c.breaker.IsOpen(target.AgentId) {
			lastErr = fmt.Errorf("circuit open for %s", target.AgentId)
			continue
		}

		invCtx, invCancel := context.WithTimeout(ctx, 10*time.Second)
		start := time.Now()
		out, err := c.out.Invoke(invCtx, target.Endpoint, capability, input, meta)
		invCancel()
		durationMs := time.Since(start).Milliseconds()

		if err != nil {
			lastErr = err
			c.breaker.RecordFailure(target.AgentId)
			c.report(ctx, target.AgentId, capability, durationMs, false)
			continue
		}

		c.breaker.RecordSuccess(target.AgentId)
		c.report(ctx, target.AgentId, capability, durationMs, true)
		return out, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no peer available")
	}
	return nil, fmt.Errorf("invoke %q failed after %d attempts: %w", capability, maxAttempts, lastErr)
}

// report fires ReportInvoke; best-effort, errors ignored.
func (c *Caller) report(ctx context.Context, peer, capability string, durationMs int64, ok bool) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, _ = c.cp.ReportInvoke(rctx, &pb.ReportInvokeRequest{
		CallerId:   c.from,
		CalleeId:   peer,
		Capability: capability,
		DurationMs: durationMs,
		Ok:         ok,
	})
}
