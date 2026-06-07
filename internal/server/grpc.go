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

package server

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentmeshv1 "github.com/neontvn/agent-mesh/api/v1"
	"github.com/neontvn/agent-mesh/internal/web"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// ControlPlaneServer implements the gRPC ControlPlane service. Embedding
// UnimplementedControlPlaneServer makes it forward-compatible with new RPCs
// added to the proto later (they default to "Unimplemented" until we
// override them here).
type ControlPlaneServer struct {
	pb.UnimplementedControlPlaneServer

	// K8sClient is the controller-runtime client used to create and update
	// Agent CRDs in response to sidecar calls. We pass in mgr.GetClient()
	// so the gRPC handlers share the same cache as the reconciler.
	K8sClient client.Client

	// Namespace where Agent CRDs are materialized. Hardcoded to "default"
	// for v0; will be configurable later.
	Namespace string

	// Bus is the event bus that fans mesh events out to UI subscribers.
	// Optional; if nil, events are simply not published.
	Bus *web.EventBus

	// rrMu protects rrCounters from concurrent SelectTarget calls.
	rrMu sync.Mutex
	// rrCounters tracks the next round-robin index to return per capability.
	// First SelectTarget call for a capability returns agents[0], next
	// returns agents[1], wrapping at the candidate count.
	rrCounters map[string]int
}

// Register handles a sidecar's initial registration. If the Agent CRD
// already exists, its spec is updated instead of returning an error —
// this makes restarting sidecars safe.
func (s *ControlPlaneServer) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	agent := &agentmeshv1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.AgentId,
			Namespace: s.Namespace,
		},
		Spec: agentmeshv1.AgentSpec{
			Capabilities: req.Capabilities,
			Endpoint:     req.Endpoint,
			Metadata:     req.Metadata,
			AgentCard:    req.AgentCard,
		},
	}

	err := s.K8sClient.Create(ctx, agent)
	if apierrors.IsAlreadyExists(err) {
		// Re-fetch the existing CRD to pick up its current resourceVersion
		// (required by the API server for optimistic concurrency), then
		// update its spec.
		var existing agentmeshv1.Agent
		if getErr := s.K8sClient.Get(ctx, client.ObjectKey{
			Namespace: s.Namespace,
			Name:      req.AgentId,
		}, &existing); getErr != nil {
			return nil, getErr
		}
		existing.Spec = agent.Spec
		if updErr := s.K8sClient.Update(ctx, &existing); updErr != nil {
			return nil, updErr
		}
	} else if err != nil {
		return nil, err
	}

	if s.Bus != nil {
		s.Bus.Publish(web.EventAgentRegistered, map[string]interface{}{
			"agent_id":     req.AgentId,
			"capabilities": req.Capabilities,
			"endpoint":     req.Endpoint,
		})
	}

	return &pb.RegisterResponse{
		LeaseId:         req.AgentId, // simple identity scheme for v0
		LeaseTtlSeconds: 30,
	}, nil
}

// Heartbeat refreshes the agent's status on its CRD. It sets lastHeartbeat
// to now and records the reported health state. Because status is a
// subresource (see kubebuilder:subresource:status marker on Agent), the
// update goes through Status().Update() — calling plain Update() would
// silently drop status changes.
func (s *ControlPlaneServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	var agent agentmeshv1.Agent
	if err := s.K8sClient.Get(ctx, client.ObjectKey{
		Namespace: s.Namespace,
		Name:      req.AgentId,
	}, &agent); err != nil {
		return nil, err
	}

	now := metav1.Now()
	agent.Status.LastHeartbeat = &now
	agent.Status.Health = healthStateToString(req.Health)

	if err := s.K8sClient.Status().Update(ctx, &agent); err != nil {
		return nil, err
	}

	if s.Bus != nil {
		// Include the full spec on every heartbeat so a UI client that
		// connects after the agent registered can still bootstrap its
		// view of the mesh from the heartbeat stream alone.
		s.Bus.Publish(web.EventAgentHeartbeat, map[string]interface{}{
			"agent_id":     req.AgentId,
			"health":       agent.Status.Health,
			"capabilities": agent.Spec.Capabilities,
			"endpoint":     agent.Spec.Endpoint,
		})
	}

	return &pb.HeartbeatResponse{
		LeaseTtlSeconds: 30,
	}, nil
}

// healthStateToString maps the proto enum to the string we store on the CRD.
func healthStateToString(h pb.HealthState) string {
	switch h {
	case pb.HealthState_HEALTH_STATE_HEALTHY:
		return "healthy"
	case pb.HealthState_HEALTH_STATE_DEGRADED:
		return "degraded"
	case pb.HealthState_HEALTH_STATE_UNHEALTHY:
		return "unhealthy"
	default:
		return ""
	}
}

// Discover returns all healthy agents advertising the requested capability.
func (s *ControlPlaneServer) Discover(ctx context.Context, req *pb.DiscoverRequest) (*pb.DiscoverResponse, error) {
	var list agentmeshv1.AgentList
	if err := s.K8sClient.List(ctx, &list, client.InNamespace(s.Namespace)); err != nil {
		return nil, err
	}

	var matches []*pb.AgentInfo
	for i := range list.Items {
		a := &list.Items[i]
		if !hasCapability(a.Spec.Capabilities, req.Capability) {
			continue
		}
		if a.Status.Health == "unhealthy" {
			continue
		}
		matches = append(matches, agentToInfo(a))
	}

	return &pb.DiscoverResponse{Agents: matches}, nil
}

// SelectTarget returns a single healthy agent for the requested capability,
// chosen by round-robin across the candidate set.
func (s *ControlPlaneServer) SelectTarget(ctx context.Context, req *pb.SelectTargetRequest) (*pb.SelectTargetResponse, error) {
	disc, err := s.Discover(ctx, &pb.DiscoverRequest{Capability: req.Capability})
	if err != nil {
		return nil, err
	}
	if len(disc.Agents) == 0 {
		return nil, status.Errorf(codes.NotFound, "no healthy agents for capability %q", req.Capability)
	}

	s.rrMu.Lock()
	if s.rrCounters == nil {
		s.rrCounters = map[string]int{}
	}
	idx := s.rrCounters[req.Capability] % len(disc.Agents)
	s.rrCounters[req.Capability]++
	s.rrMu.Unlock()

	return &pb.SelectTargetResponse{Agent: disc.Agents[idx]}, nil
}

// hasCapability reports whether `caps` contains `want`.
func hasCapability(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// agentToInfo converts an Agent CRD to the AgentInfo wire type.
func agentToInfo(a *agentmeshv1.Agent) *pb.AgentInfo {
	return &pb.AgentInfo{
		AgentId:      a.Name,
		Capabilities: a.Spec.Capabilities,
		Endpoint:     a.Spec.Endpoint,
		Metadata:     a.Spec.Metadata,
		AgentCard:    a.Spec.AgentCard,
	}
}

// ReportInvoke records an A2A invocation that has just completed and
// broadcasts the event to observability subscribers (the live UI).
func (s *ControlPlaneServer) ReportInvoke(ctx context.Context, req *pb.ReportInvokeRequest) (*pb.ReportInvokeResponse, error) {
	if s.Bus != nil {
		s.Bus.Publish(web.EventInvokeCompleted, map[string]interface{}{
			"caller_id":     req.CallerId,
			"callee_id":     req.CalleeId,
			"capability":    req.Capability,
			"duration_ms":   req.DurationMs,
			"ok":            req.Ok,
			"method":        req.Method,
			"payload_bytes": req.PayloadBytes,
		})
	}
	return &pb.ReportInvokeResponse{}, nil
}

// ReportTaskEvent records an A2A task state transition reported by the agent
// running the task and broadcasts it to observability subscribers (the live UI).
func (s *ControlPlaneServer) ReportTaskEvent(ctx context.Context, req *pb.ReportTaskEventRequest) (*pb.ReportTaskEventResponse, error) {
	if s.Bus != nil {
		s.Bus.Publish(web.EventTaskUpdated, map[string]interface{}{
			"agent_id":   req.AgentId,
			"task_id":    req.TaskId,
			"context_id": req.ContextId,
			"capability": req.Capability,
			"state":      req.State,
		})
	}
	return &pb.ReportTaskEventResponse{}, nil
}
