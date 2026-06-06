package sidecar

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPLocalAgentForwards(t *testing.T) {
	var gotCapability, gotMeta, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCapability = r.Header.Get("X-AgentMesh-Capability")
		gotMeta = r.Header.Get("X-AgentMesh-Meta-trace")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("agent-result"))
	}))
	defer srv.Close()

	agent := newHTTPLocalAgent(srv.URL, &http.Client{Timeout: 5 * time.Second})
	out, err := agent.Invoke(context.Background(), "summarize", []byte("hello"), map[string]string{"trace": "abc"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	if string(out) != "agent-result" {
		t.Errorf("result = %q, want agent-result", out)
	}
	if gotCapability != "summarize" {
		t.Errorf("capability header = %q, want summarize", gotCapability)
	}
	if gotMeta != "abc" {
		t.Errorf("meta header = %q, want abc", gotMeta)
	}
	if gotBody != "hello" {
		t.Errorf("forwarded body = %q, want hello", gotBody)
	}
}

func TestHTTPLocalAgentNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	agent := newHTTPLocalAgent(srv.URL, &http.Client{Timeout: 5 * time.Second})
	if _, err := agent.Invoke(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("expected error on non-200 response, got nil")
	}
}
