package sidecar

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// fakeCP is a stub pb.ControlPlaneClient: SelectTarget returns a fixed target,
// ReportInvoke is counted. Other methods are unused by Caller.
type fakeCP struct {
	pb.ControlPlaneClient
	target  *pb.AgentInfo
	selErr  error
	reports int
}

func (f *fakeCP) SelectTarget(context.Context, *pb.SelectTargetRequest, ...grpc.CallOption) (*pb.SelectTargetResponse, error) {
	if f.selErr != nil {
		return nil, f.selErr
	}
	return &pb.SelectTargetResponse{Agent: f.target}, nil
}

func (f *fakeCP) ReportInvoke(context.Context, *pb.ReportInvokeRequest, ...grpc.CallOption) (*pb.ReportInvokeResponse, error) {
	f.reports++
	return &pb.ReportInvokeResponse{}, nil
}

// fakeOutbound is a stub dataplane.Outbound.
type fakeOutbound struct {
	result []byte
	err    error
	calls  int
}

func (f *fakeOutbound) Invoke(context.Context, string, string, []byte, map[string]string) ([]byte, error) {
	f.calls++
	return f.result, f.err
}

func (f *fakeOutbound) Close() error { return nil }

func TestCallerInvokeSuccess(t *testing.T) {
	cp := &fakeCP{target: &pb.AgentInfo{AgentId: "searcher-1", Endpoint: "http://x/a2a"}}
	out := &fakeOutbound{result: []byte("results")}

	c := NewCaller(cp, out, "planner")
	res, err := c.Invoke(context.Background(), "search", []byte("query"), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(res) != "results" {
		t.Errorf("result = %q, want results", res)
	}
	if out.calls != 1 {
		t.Errorf("outbound calls = %d, want 1", out.calls)
	}
	if cp.reports != 1 {
		t.Errorf("ReportInvoke calls = %d, want 1", cp.reports)
	}
}

func TestCallerOpensBreakerAfterFailures(t *testing.T) {
	cp := &fakeCP{target: &pb.AgentInfo{AgentId: "searcher-1", Endpoint: "http://x/a2a"}}
	out := &fakeOutbound{err: errors.New("peer down")}

	c := NewCaller(cp, out, "planner")
	_, err := c.Invoke(context.Background(), "search", nil, nil)
	if err == nil {
		t.Fatal("expected error after repeated failures, got nil")
	}
	// Breaker opens after 3 failures, so the peer is dialed exactly 3 times;
	// the remaining 2 attempts are skipped because the circuit is open.
	if out.calls != 3 {
		t.Errorf("outbound calls = %d, want 3 (then breaker open)", out.calls)
	}
	if !c.breaker.IsOpen("searcher-1") {
		t.Error("expected circuit open for searcher-1")
	}
}
