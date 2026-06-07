// Package a2adp is the A2A (HTTPS + JSON-RPC 2.0) implementation of the sidecar
// data plane. It satisfies dataplane.Inbound (Server). It serves the agent card,
// handles message/send, tasks/get, and tasks/cancel, and streams task updates
// over SSE for message/stream, using the wire types in internal/a2a.
package a2adp

import (
	"sync"

	"github.com/neontvn/agent-mesh/internal/a2a"
)

// TaskStore holds tasks by ID. v1 is in-memory; a durable implementation can
// satisfy the same interface later (deferred per DESIGN.md §5.4).
type TaskStore interface {
	Put(t a2a.Task)
	Get(id string) (a2a.Task, bool)
}

type memStore struct {
	mu    sync.RWMutex
	tasks map[string]a2a.Task
}

var _ TaskStore = (*memStore)(nil)

func newMemStore() *memStore {
	return &memStore{tasks: map[string]a2a.Task{}}
}

func (m *memStore) Put(t a2a.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
}

func (m *memStore) Get(id string) (a2a.Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	return t, ok
}
