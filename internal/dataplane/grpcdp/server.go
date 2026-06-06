// Package grpcdp is the gRPC implementation of the sidecar data plane. It
// satisfies dataplane.Inbound (Server) and dataplane.Outbound (Client). This is
// the legacy transport; the A2A transport will live in a sibling package and
// implement the same interfaces.
package grpcdp

import (
	"context"
	"errors"
	"log"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/neontvn/agent-mesh/internal/dataplane"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// Server is a gRPC dataplane.Inbound. It exposes the AgentDataPlane service and
// delegates each Invoke to the configured LocalAgent.
type Server struct {
	pb.UnimplementedAgentDataPlaneServer
	agent dataplane.LocalAgent
}

var _ dataplane.Inbound = (*Server)(nil)

// NewServer returns a gRPC inbound transport. The LocalAgent is provided at
// Serve time.
func NewServer() *Server { return &Server{} }

// Serve starts the gRPC server on listenAddr, dispatching inbound calls to
// agent, and blocks until ctx is canceled or the server stops.
func (s *Server) Serve(ctx context.Context, listenAddr string, agent dataplane.LocalAgent) error {
	s.agent = agent

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	srv := grpclib.NewServer(grpclib.StatsHandler(otelgrpc.NewServerHandler()))
	pb.RegisterAgentDataPlaneServer(srv, s)
	reflection.Register(srv)

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	log.Printf("[dataplane/grpc] listening for A2A calls on %s", listenAddr)
	if err := srv.Serve(lis); err != nil && !errors.Is(err, grpclib.ErrServerStopped) {
		return err
	}
	return nil
}

// Invoke implements the AgentDataPlane gRPC service by forwarding to the local
// agent. The agent's response becomes the gRPC response payload.
func (s *Server) Invoke(ctx context.Context, req *pb.InvokeRequest) (*pb.InvokeResponse, error) {
	out, err := s.agent.Invoke(ctx, req.Capability, req.Payload, req.Metadata)
	if err != nil {
		return nil, err
	}
	return &pb.InvokeResponse{Payload: out}, nil
}
