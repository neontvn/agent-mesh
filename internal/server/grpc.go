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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentmeshv1 "github.com/neontvn/agent-mesh/api/v1"
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
