package a2adp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// startServer spins up an in-process A2A server backed by agent and returns its
// base URL — used to exercise the Client against the real Server end to end.
func startServer(t *testing.T, agent *fakeAgent) string {
	t.Helper()
	s := NewServer(BuildCard("summarizer", "test agent", "", "0.1.0", []string{"summarize"}))
	s.agent = agent
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestClientServerRoundTrip(t *testing.T) {
	agent := &fakeAgent{result: []byte("the summary")}
	url := startServer(t, agent)

	out, err := NewClient().Invoke(context.Background(), url, "summarize",
		[]byte("text to summarize"), map[string]string{"trace": "abc"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if string(out) != "the summary" {
		t.Errorf("result = %q, want 'the summary'", out)
	}
	if agent.gotCap != "summarize" {
		t.Errorf("peer received capability %q, want summarize (should travel in metadata)", agent.gotCap)
	}
}

func TestClientRoundTripData(t *testing.T) {
	agent := &fakeAgent{result: []byte(`{"score":0.9}`)}
	url := startServer(t, agent)

	out, err := NewClient().Invoke(context.Background(), url, "summarize", []byte(`{"q":"hi"}`), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("result is not JSON: %v (%s)", err, out)
	}
	if got["score"] != 0.9 {
		t.Errorf("score = %v, want 0.9", got["score"])
	}
}

func TestClientPropagatesTaskFailure(t *testing.T) {
	agent := &fakeAgent{err: errors.New("agent boom")}
	url := startServer(t, agent)

	_, err := NewClient().Invoke(context.Background(), url, "summarize", []byte("x"), nil)
	if err == nil {
		t.Fatal("expected error from failed task, got nil")
	}
	if !strings.Contains(err.Error(), "agent boom") {
		t.Errorf("error = %v, want it to contain 'agent boom'", err)
	}
}

func TestRPCURL(t *testing.T) {
	cases := map[string]string{
		"localhost:9090":          "http://localhost:9090/a2a",
		"http://host:9090":        "http://host:9090/a2a",
		"http://host:9090/":       "http://host:9090/a2a",
		"https://host/a2a":        "https://host/a2a",
		"http://host:9090/a2a":    "http://host:9090/a2a",
	}
	for in, want := range cases {
		if got := rpcURL(in); got != want {
			t.Errorf("rpcURL(%q) = %q, want %q", in, got, want)
		}
	}
}
