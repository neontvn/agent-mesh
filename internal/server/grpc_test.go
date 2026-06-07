package server

import (
	"context"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentmeshv1 "github.com/neontvn/agent-mesh/api/v1"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// newTestServer builds a ControlPlaneServer backed by a fake Kubernetes client,
// so the control-plane logic can be tested without an envtest control plane.
func newTestServer(t *testing.T) *ControlPlaneServer {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := agentmeshv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&agentmeshv1.Agent{}).
		Build()
	return &ControlPlaneServer{K8sClient: c, Namespace: "default"}
}

func TestRegisterStoresAndDiscoverServesAgentCard(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	card := `{"name":"summarizer","skills":[{"id":"summarize","name":"Summarize"}]}`
	if _, err := s.Register(ctx, &pb.RegisterRequest{
		AgentId:      "summarizer-1",
		Capabilities: []string{"summarize"},
		Endpoint:     "http://summarizer:9090",
		AgentCard:    card,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// The card must be persisted on the CRD spec.
	var stored agentmeshv1.Agent
	if err := s.K8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "summarizer-1"}, &stored); err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if stored.Spec.AgentCard != card {
		t.Errorf("stored card = %q, want %q", stored.Spec.AgentCard, card)
	}

	// Mark healthy so Discover returns it, then check the card flows through.
	if _, err := s.Heartbeat(ctx, &pb.HeartbeatRequest{
		AgentId: "summarizer-1",
		Health:  pb.HealthState_HEALTH_STATE_HEALTHY,
	}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	disc, err := s.Discover(ctx, &pb.DiscoverRequest{Capability: "summarize"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(disc.Agents) != 1 {
		t.Fatalf("discovered %d agents, want 1", len(disc.Agents))
	}
	if disc.Agents[0].AgentCard != card {
		t.Errorf("discovered card = %q, want %q", disc.Agents[0].AgentCard, card)
	}

	// And the served card must parse back into the wire type.
	var parsed struct {
		Name   string `json:"name"`
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(disc.Agents[0].AgentCard), &parsed); err != nil {
		t.Fatalf("served card is not valid JSON: %v", err)
	}
	if parsed.Name != "summarizer" || len(parsed.Skills) != 1 || parsed.Skills[0].ID != "summarize" {
		t.Errorf("parsed card = %+v, unexpected", parsed)
	}
}
