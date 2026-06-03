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

// Register handles a sidecar's initial registration. It creates an Agent
// CRD reflecting the sidecar's declared capabilities and returns a lease
// handle the sidecar must refresh via Heartbeat.
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

	if err := s.K8sClient.Create(ctx, agent); err != nil {
		return nil, err
	}

	return &pb.RegisterResponse{
		LeaseId:         req.AgentId, // simple identity scheme for v0
		LeaseTtlSeconds: 30,
	}, nil
}

// Heartbeat will be fully implemented in M2.4. For now it's a stub that
// just acknowledges the call so the sidecar can wire up the loop
// end-to-end.
func (s *ControlPlaneServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	return &pb.HeartbeatResponse{
		LeaseTtlSeconds: 30,
	}, nil
}
