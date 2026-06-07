package a2adp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/neontvn/agent-mesh/internal/a2a"
	"github.com/neontvn/agent-mesh/internal/dataplane"
)

// Client is an A2A (HTTPS + JSON-RPC 2.0) implementation of dataplane.Outbound.
// It sends message/send to a peer's /a2a endpoint and returns the resulting
// artifact bytes. The HTTP transport is wrapped with otelhttp so the caller's
// trace context propagates to the peer as W3C traceparent headers.
type Client struct {
	http *http.Client
}

var _ dataplane.Outbound = (*Client)(nil)

// NewClient returns an A2A outbound transport.
func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Invoke sends the payload to the peer as an A2A message/send, blocks for the
// resulting Task, and returns the bytes of its first artifact part. A failed or
// rejected task is surfaced as an error. The capability is carried in the
// message metadata so the peer can route to the right local skill.
func (c *Client) Invoke(ctx context.Context, endpoint, capability string, payload []byte, meta map[string]string) ([]byte, error) {
	msg := a2a.Message{
		Role:      a2a.RoleUser,
		MessageID: newID(),
		Kind:      "message",
		Parts:     []a2a.Part{partFromBytes(payload)},
		Metadata:  buildMeta(capability, meta),
	}

	params, err := json.Marshal(messageSendParams{Message: msg})
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	reqBody, err := json.Marshal(a2a.Request{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      newID(),
		Method:  a2a.MethodMessageSend,
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL(endpoint), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", endpoint, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer returned HTTP %d", httpResp.StatusCode)
	}

	var rpcResp a2a.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("a2a error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	var task a2a.Task
	if err := json.Unmarshal(rpcResp.Result, &task); err != nil {
		return nil, fmt.Errorf("decode task: %w", err)
	}
	return taskResult(task)
}

// Close is a no-op; the underlying http.Client manages idle connections itself.
func (c *Client) Close() error { return nil }

// buildMeta merges the capability and any string metadata into the A2A message
// metadata map. Returns nil when empty.
func buildMeta(capability string, meta map[string]string) map[string]any {
	m := map[string]any{}
	if capability != "" {
		m["capability"] = capability
	}
	for k, v := range meta {
		m[k] = v
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// rpcURL normalizes a registered endpoint into the peer's /a2a JSON-RPC URL,
// defaulting to http:// when no scheme is present.
func rpcURL(endpoint string) string {
	u := endpoint
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "http://" + u
	}
	u = strings.TrimRight(u, "/")
	if strings.HasSuffix(u, "/a2a") {
		return u
	}
	return u + "/a2a"
}

// taskResult extracts the result bytes from a terminal Task, or an error if the
// task did not complete successfully.
func taskResult(task a2a.Task) ([]byte, error) {
	switch task.Status.State {
	case a2a.TaskStateCompleted:
		if len(task.Artifacts) > 0 && len(task.Artifacts[0].Parts) > 0 {
			return partBytes(task.Artifacts[0].Parts[0]), nil
		}
		return nil, nil
	case a2a.TaskStateFailed, a2a.TaskStateRejected:
		return nil, fmt.Errorf("task %s: %s", task.Status.State, statusText(task.Status))
	default:
		return nil, fmt.Errorf("task ended in non-terminal state %q", task.Status.State)
	}
}

// partBytes renders a Part back to bytes: text as-is, data re-marshaled to JSON.
func partBytes(p a2a.Part) []byte {
	switch p.Kind {
	case a2a.PartKindText:
		return []byte(p.Text)
	case a2a.PartKindData:
		if b, err := json.Marshal(p.Data); err == nil {
			return b
		}
	}
	return nil
}

// statusText pulls a human-readable string from a status message, falling back
// to the state name.
func statusText(st a2a.TaskStatus) string {
	if st.Message != nil {
		var sb strings.Builder
		for _, p := range st.Message.Parts {
			if p.Kind == a2a.PartKindText {
				sb.WriteString(p.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return string(st.State)
}
