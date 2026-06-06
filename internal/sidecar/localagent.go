package sidecar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/neontvn/agent-mesh/internal/dataplane"
)

// httpLocalAgent dispatches inbound capability calls to the local agent
// application over HTTP. The sidecar is pure transport: it POSTs the payload
// and returns the agent's response body unchanged.
//
// HTTP forwarding contract:
//   - POST <forwardURL>
//   - Content-Type: application/json
//   - X-AgentMesh-Capability: <capability>
//   - X-AgentMesh-Meta-<k>: <v>   (one header per metadata pair)
//   - body: payload, passed through unchanged
//   - HTTP 200 → body becomes the result; any non-200 → error
type httpLocalAgent struct {
	forwardURL string
	httpClient *http.Client
}

var _ dataplane.LocalAgent = (*httpLocalAgent)(nil)

func newHTTPLocalAgent(forwardURL string, client *http.Client) *httpLocalAgent {
	return &httpLocalAgent{forwardURL: forwardURL, httpClient: client}
}

func (a *httpLocalAgent) Invoke(ctx context.Context, capability string, payload []byte, meta map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.forwardURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentMesh-Capability", capability)
	for k, v := range meta {
		req.Header.Set("X-AgentMesh-Meta-"+k, v)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("forward to %s: %w", a.forwardURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read agent response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}
