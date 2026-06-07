package a2adp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/neontvn/agent-mesh/internal/a2a"
)

// fakeAgent is a stub dataplane.LocalAgent for tests.
type fakeAgent struct {
	result []byte
	err    error
	gotCap string
}

func (f *fakeAgent) Invoke(_ context.Context, capability string, _ []byte, _ map[string]string) ([]byte, error) {
	f.gotCap = capability
	return f.result, f.err
}

func newTestServer(t *testing.T, agent *fakeAgent) *httptest.Server {
	t.Helper()
	s := NewServer(BuildCard("summarizer", "test agent", "http://x/a2a", "0.1.0", []string{"summarize"}))
	s.agent = agent
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return b
}

func postRPC(t *testing.T, url string, req a2a.Request) a2a.Response {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(url+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()
	var out a2a.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func sendMessage(t *testing.T, url, text string) a2a.Task {
	t.Helper()
	resp := postRPC(t, url, a2a.Request{
		JSONRPC: "2.0", ID: 1, Method: a2a.MethodMessageSend,
		Params: mustRaw(t, messageSendParams{Message: a2a.Message{
			Role: a2a.RoleUser, MessageID: "m1", Kind: "message",
			Parts: []a2a.Part{a2a.NewTextPart(text)},
		}}),
	})
	if resp.Error != nil {
		t.Fatalf("message/send error: %+v", resp.Error)
	}
	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task
}

func TestAgentCardEndpoint(t *testing.T) {
	ts := newTestServer(t, &fakeAgent{result: []byte("ok")})

	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("GET card: %v", err)
	}
	defer resp.Body.Close()

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if card.Name != "summarizer" {
		t.Errorf("name = %q, want summarizer", card.Name)
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "summarize" {
		t.Errorf("skills = %#v, want one 'summarize'", card.Skills)
	}
}

func TestMessageSendCompletesWithArtifact(t *testing.T) {
	agent := &fakeAgent{result: []byte("a short summary")}
	ts := newTestServer(t, agent)

	task := sendMessage(t, ts.URL, "summarize this please")

	if task.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", task.Status.State)
	}
	if agent.gotCap != "summarize" {
		t.Errorf("local agent capability = %q, want summarize", agent.gotCap)
	}
	if len(task.Artifacts) != 1 || len(task.Artifacts[0].Parts) != 1 {
		t.Fatalf("artifacts = %#v, want one artifact with one part", task.Artifacts)
	}
	if got := task.Artifacts[0].Parts[0].Text; got != "a short summary" {
		t.Errorf("artifact text = %q, want 'a short summary'", got)
	}
}

func TestTasksGetReturnsStoredTask(t *testing.T) {
	ts := newTestServer(t, &fakeAgent{result: []byte("done")})
	task := sendMessage(t, ts.URL, "hi")

	resp := postRPC(t, ts.URL, a2a.Request{
		JSONRPC: "2.0", ID: 2, Method: a2a.MethodTasksGet,
		Params: mustRaw(t, taskIDParams{ID: task.ID}),
	})
	if resp.Error != nil {
		t.Fatalf("tasks/get error: %+v", resp.Error)
	}
	var got a2a.Task
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if got.ID != task.ID || got.Status.State != a2a.TaskStateCompleted {
		t.Errorf("got id=%q state=%q, want id=%q completed", got.ID, got.Status.State, task.ID)
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	ts := newTestServer(t, &fakeAgent{})
	resp := postRPC(t, ts.URL, a2a.Request{JSONRPC: "2.0", ID: 3, Method: "bogus/method"})
	if resp.Error == nil || resp.Error.Code != a2a.CodeMethodNotFound {
		t.Fatalf("error = %+v, want MethodNotFound", resp.Error)
	}
}

func TestTasksGetMissingIsNotFound(t *testing.T) {
	ts := newTestServer(t, &fakeAgent{})
	resp := postRPC(t, ts.URL, a2a.Request{
		JSONRPC: "2.0", ID: 4, Method: a2a.MethodTasksGet,
		Params: mustRaw(t, taskIDParams{ID: "nope"}),
	})
	if resp.Error == nil || resp.Error.Code != a2a.CodeTaskNotFound {
		t.Fatalf("error = %+v, want TaskNotFound", resp.Error)
	}
}

func TestMessageStreamEmitsLifecycle(t *testing.T) {
	ts := newTestServer(t, &fakeAgent{result: []byte("streamed result")})

	body, _ := json.Marshal(a2a.Request{
		JSONRPC: "2.0", ID: 5, Method: a2a.MethodMessageStream,
		Params: mustRaw(t, messageSendParams{Message: a2a.Message{
			Role: a2a.RoleUser, MessageID: "m1", Kind: "message",
			Parts: []a2a.Part{a2a.NewTextPart("go")},
		}}),
	})
	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST stream: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read stream: %v", err)
	}
	stream := buf.String()
	for _, want := range []string{"submitted", "working", "completed", "streamed result"} {
		if !strings.Contains(stream, want) {
			t.Errorf("stream missing %q\n---\n%s", want, stream)
		}
	}
}
