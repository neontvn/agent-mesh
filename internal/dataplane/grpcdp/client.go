package grpcdp

import (
	"context"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/neontvn/agent-mesh/internal/dataplane"
	pb "github.com/neontvn/agent-mesh/proto/agentmesh/v1"
)

// Client is a gRPC dataplane.Outbound. It pools one connection per peer
// endpoint so loop-mode invocations don't redial each time.
type Client struct {
	mu    sync.Mutex
	conns map[string]*grpclib.ClientConn
}

var _ dataplane.Outbound = (*Client)(nil)

// NewClient returns a gRPC outbound transport with an empty connection pool.
func NewClient() *Client {
	return &Client{conns: map[string]*grpclib.ClientConn{}}
}

// Invoke dials (or reuses) the peer at endpoint and calls its AgentDataPlane
// Invoke, returning the response payload.
func (c *Client) Invoke(ctx context.Context, endpoint, capability string, payload []byte, meta map[string]string) ([]byte, error) {
	conn, err := c.dial(endpoint)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewAgentDataPlaneClient(conn).Invoke(ctx, &pb.InvokeRequest{
		Capability: capability,
		Payload:    payload,
		Metadata:   meta,
	})
	if err != nil {
		return nil, err
	}
	return resp.Payload, nil
}

// dial returns the pooled connection for endpoint, creating one if needed.
// grpc.NewClient is lazy: connection errors surface on the first RPC, not here.
func (c *Client) dial(endpoint string) (*grpclib.ClientConn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, ok := c.conns[endpoint]; ok {
		return conn, nil
	}
	conn, err := grpclib.NewClient(
		endpoint,
		grpclib.WithTransportCredentials(insecure.NewCredentials()),
		grpclib.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	c.conns[endpoint] = conn
	return conn, nil
}

// Method reports the wire method this transport uses.
func (c *Client) Method() string { return "grpc/invoke" }

// Close releases all pooled connections.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.conns {
		_ = conn.Close()
	}
	c.conns = map[string]*grpclib.ClientConn{}
	return nil
}
