package a2a

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPartRoundTrip(t *testing.T) {
	cases := map[string]Part{
		"text": NewTextPart("hello world"),
		"data": NewDataPart(map[string]any{"key": "value", "n": float64(3)}),
		"file": NewFilePart(&FileContent{Name: "a.txt", MimeType: "text/plain", URI: "https://example.com/a.txt"}),
	}
	for name, want := range cases {
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var got Part
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: round-trip mismatch:\n want %#v\n got  %#v", name, want, got)
		}
	}
}

func TestTaskStateIsTerminal(t *testing.T) {
	terminal := []TaskState{TaskStateCompleted, TaskStateCanceled, TaskStateFailed, TaskStateRejected}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	nonTerminal := []TaskState{TaskStateSubmitted, TaskStateWorking, TaskStateInputRequired, TaskStateAuthRequired, TaskStateUnknown}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

func TestTaskRoundTrip(t *testing.T) {
	want := Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Kind:      "task",
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: "2026-06-06T00:00:00Z",
		},
		Artifacts: []Artifact{{
			ArtifactID: "art-1",
			Name:       "guide.md",
			Parts:      []Part{NewTextPart("# Interview Guide")},
		}},
		History: []Message{{
			Role:      RoleUser,
			MessageID: "msg-1",
			Kind:      "message",
			Parts:     []Part{NewTextPart("build me a guide")},
		}},
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Task
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("task round-trip mismatch:\n want %#v\n got  %#v", want, got)
	}
}

func TestAgentCardParses(t *testing.T) {
	const sample = `{
                "protocolVersion": "1.0.1",
                "name": "Summarizer",
                "description": "Summarizes text",
                "url": "https://summarizer.local/a2a",
                "version": "0.1.0",
                "capabilities": {"streaming": true},
                "defaultInputModes": ["text/plain"],
                "defaultOutputModes": ["text/plain"],
                "skills": [
                        {"id": "summarize", "name": "Summarize", "description": "Condense text", "tags": ["text"]}
                ]
        }`
	var card AgentCard
	if err := json.Unmarshal([]byte(sample), &card); err != nil {
		t.Fatalf("unmarshal agent card: %v", err)
	}
	if card.Name != "Summarizer" {
		t.Errorf("name = %q, want Summarizer", card.Name)
	}
	if !card.Capabilities.Streaming {
		t.Errorf("expected streaming capability")
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "summarize" {
		t.Errorf("skills = %#v, want one skill 'summarize'", card.Skills)
	}
}
