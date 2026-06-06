package a2a

// TaskState is the lifecycle state of a Task, as its A2A JSON string value.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateFailed        TaskState = "failed"
	TaskStateRejected      TaskState = "rejected"
	TaskStateAuthRequired  TaskState = "auth-required"
	TaskStateUnknown       TaskState = "unknown"
)

// IsTerminal reports whether the state is final, i.e. no further transitions
// are expected.
func (s TaskState) IsTerminal() bool {
	switch s {
	case TaskStateCompleted, TaskStateCanceled, TaskStateFailed, TaskStateRejected:
		return true
	default:
		return false
	}
}

// TaskStatus is the current status of a Task plus an optional status message
// (e.g. the question an agent asks when it enters input-required).
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"` // RFC3339 / ISO 8601
}

// Task is a unit of work an agent tracks through its lifecycle. The server
// generates ID; ContextID groups related tasks into one logical interaction.
type Task struct {
	ID        string         `json:"id"`
	ContextID string         `json:"contextId,omitempty"`
	Status    TaskStatus     `json:"status"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	History   []Message      `json:"history,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Kind      string         `json:"kind,omitempty"` // discriminator: "task"
}
