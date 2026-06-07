package a2adp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/neontvn/agent-mesh/internal/a2a"
	"github.com/neontvn/agent-mesh/internal/dataplane"
)

// Server is an A2A (HTTPS + JSON-RPC 2.0) implementation of dataplane.Inbound.
// It serves the agent card and dispatches message/send, message/stream,
// tasks/get, and tasks/cancel. Task state is held in memory.
//
// Lifecycle adapter: the local agent is one-shot (one call, one result), so a
// task moves submitted -> working -> completed (or failed) within a single
// message/send. True mid-flight cancellation needs async execution and is out
// of scope for v1 (tasks/cancel only succeeds before a task reaches a terminal
// state, which in practice means it is a no-op for the synchronous path).
type Server struct {
	card  a2a.AgentCard
	tasks *memStore
	agent dataplane.LocalAgent
}

var _ dataplane.Inbound = (*Server)(nil)

// NewServer returns an A2A inbound transport serving the given card. The
// LocalAgent is supplied at Serve time (per the dataplane.Inbound contract).
func NewServer(card a2a.AgentCard) *Server {
	return &Server{card: card, tasks: newMemStore()}
}

// Handler returns the bare HTTP routes, without OTel wrapping. Exposed for tests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/agent-card.json", s.handleAgentCard)
	mux.HandleFunc("POST /a2a", s.handleRPC)
	return mux
}

// Serve starts the A2A HTTP server, dispatching inbound calls to agent, and
// blocks until ctx is canceled or the server stops. The handler is wrapped with
// otelhttp so incoming W3C trace context continues the caller's trace.
func (s *Server) Serve(ctx context.Context, listenAddr string, agent dataplane.LocalAgent) error {
	s.agent = agent

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: otelhttp.NewHandler(s.Handler(), "a2a-inbound"),
	}

	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	log.Printf("[dataplane/a2a] serving A2A on %s (agent %q, %d skills)",
		listenAddr, s.card.Name, len(s.card.Skills))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.card)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req a2a.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, a2a.CodeParseError, "parse error")
		return
	}
	switch req.Method {
	case a2a.MethodMessageSend:
		s.handleMessageSend(r.Context(), w, &req)
	case a2a.MethodMessageStream:
		s.handleMessageStream(r.Context(), w, &req)
	case a2a.MethodTasksGet:
		s.handleTasksGet(w, &req)
	case a2a.MethodTasksCancel:
		s.handleTasksCancel(w, &req)
	default:
		writeRPCError(w, req.ID, a2a.CodeMethodNotFound, "method not found: "+req.Method)
	}
}

// messageSendParams is the params shape for message/send and message/stream.
type messageSendParams struct {
	Message a2a.Message `json:"message"`
}

// taskIDParams is the params shape for tasks/get and tasks/cancel.
type taskIDParams struct {
	ID string `json:"id"`
}

func (s *Server) handleMessageSend(ctx context.Context, w http.ResponseWriter, req *a2a.Request) {
	var p messageSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, a2a.CodeInvalidParams, "invalid params: "+err.Error())
		return
	}
	task := s.runTask(ctx, p.Message, nil)
	writeRPCResult(w, req.ID, task)
}

func (s *Server) handleMessageStream(ctx context.Context, w http.ResponseWriter, req *a2a.Request) {
	var p messageSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, a2a.CodeInvalidParams, "invalid params: "+err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRPCError(w, req.ID, a2a.CodeInternalError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Emit each state transition as an SSE event carrying a JSON-RPC response
	// whose result is the current Task snapshot.
	emit := func(t a2a.Task) {
		resp := a2a.Response{JSONRPC: a2a.JSONRPCVersion, ID: req.ID}
		if raw, err := json.Marshal(t); err == nil {
			resp.Result = raw
		}
		if data, err := json.Marshal(resp); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	s.runTask(ctx, p.Message, emit)
}

func (s *Server) handleTasksGet(w http.ResponseWriter, req *a2a.Request) {
	var p taskIDParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, a2a.CodeInvalidParams, "invalid params")
		return
	}
	t, ok := s.tasks.Get(p.ID)
	if !ok {
		writeRPCError(w, req.ID, a2a.CodeTaskNotFound, "task not found: "+p.ID)
		return
	}
	writeRPCResult(w, req.ID, t)
}

func (s *Server) handleTasksCancel(w http.ResponseWriter, req *a2a.Request) {
	var p taskIDParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, a2a.CodeInvalidParams, "invalid params")
		return
	}
	t, ok := s.tasks.Get(p.ID)
	if !ok {
		writeRPCError(w, req.ID, a2a.CodeTaskNotFound, "task not found: "+p.ID)
		return
	}
	if t.Status.State.IsTerminal() {
		writeRPCError(w, req.ID, a2a.CodeTaskNotCancelable, "task already in terminal state")
		return
	}
	t.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: now()}
	s.tasks.Put(t)
	writeRPCResult(w, req.ID, t)
}

// runTask drives a one-shot LocalAgent call through the A2A task lifecycle,
// storing each transition. If emit is non-nil it is called on every transition
// (used by message/stream). It returns the final Task.
func (s *Server) runTask(ctx context.Context, msg a2a.Message, emit func(a2a.Task)) a2a.Task {
	notify := func(t a2a.Task) {
		s.tasks.Put(t)
		if emit != nil {
			emit(t)
		}
	}

	id := newID()
	contextID := msg.ContextID
	if contextID == "" {
		contextID = newID()
	}

	task := a2a.Task{
		ID:        id,
		ContextID: contextID,
		Kind:      "task",
		Status:    a2a.TaskStatus{State: a2a.TaskStateSubmitted, Timestamp: now()},
		History:   []a2a.Message{msg},
	}
	notify(task)

	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: now()}
	notify(task)

	capability := resolveCapability(msg, s.card)
	out, err := s.agent.Invoke(ctx, capability, messageInput(msg), stringMeta(msg.Metadata))
	if err != nil {
		task.Status = a2a.TaskStatus{
			State:     a2a.TaskStateFailed,
			Timestamp: now(),
			Message:   errMessage(contextID, id, err),
		}
		notify(task)
		return task
	}

	task.Artifacts = []a2a.Artifact{{
		ArtifactID: newID(),
		Name:       capability + "-result",
		Parts:      []a2a.Part{partFromBytes(out)},
	}}
	task.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Timestamp: now()}
	notify(task)
	return task
}

// resolveCapability picks the local capability to invoke: an explicit
// "capability"/"skill" entry in the message metadata wins; otherwise the
// card's first advertised skill is used.
func resolveCapability(msg a2a.Message, card a2a.AgentCard) string {
	if c := metaString(msg.Metadata, "capability"); c != "" {
		return c
	}
	if c := metaString(msg.Metadata, "skill"); c != "" {
		return c
	}
	if len(card.Skills) > 0 {
		return card.Skills[0].ID
	}
	return ""
}

// messageInput extracts the payload bytes for the local agent from the first
// data or text part of the message.
func messageInput(msg a2a.Message) []byte {
	for _, p := range msg.Parts {
		switch p.Kind {
		case a2a.PartKindData:
			if b, err := json.Marshal(p.Data); err == nil {
				return b
			}
		case a2a.PartKindText:
			return []byte(p.Text)
		}
	}
	return nil
}

// partFromBytes wraps raw bytes as a Part: structured JSON becomes a data part,
// anything else a text part. Shared by the inbound artifact path and the
// outbound request path.
func partFromBytes(out []byte) a2a.Part {
	var v any
	if json.Unmarshal(out, &v) == nil {
		switch v.(type) {
		case map[string]any, []any:
			return a2a.NewDataPart(v)
		}
	}
	return a2a.NewTextPart(string(out))
}

func errMessage(contextID, taskID string, err error) *a2a.Message {
	return &a2a.Message{
		Role:      a2a.RoleAgent,
		MessageID: newID(),
		ContextID: contextID,
		TaskID:    taskID,
		Kind:      "message",
		Parts:     []a2a.Part{a2a.NewTextPart(err.Error())},
	}
}

func metaString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func stringMeta(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		writeRPCError(w, id, a2a.CodeInternalError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a2a.Response{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      id,
		Result:  raw,
	})
}

// writeRPCError writes a JSON-RPC error. Per JSON-RPC 2.0 the HTTP status stays
// 200; the error lives in the response body.
func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	writeJSON(w, http.StatusOK, a2a.Response{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      id,
		Error:   &a2a.Error{Code: code, Message: msg},
	})
}
