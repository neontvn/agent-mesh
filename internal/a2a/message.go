package a2a

import (
	"encoding/json"
	"fmt"
)

// Role identifies the sender of a Message.
type Role string

const (
	RoleUser  Role = "user"
	RoleAgent Role = "agent"
)

// Message is a single turn in the interaction between a client and an agent.
// Parts carry the actual content.
type Message struct {
	Role             Role           `json:"role"`
	Parts            []Part         `json:"parts"`
	MessageID        string         `json:"messageId"`
	ContextID        string         `json:"contextId,omitempty"`
	TaskID           string         `json:"taskId,omitempty"`
	ReferenceTaskIDs []string       `json:"referenceTaskIds,omitempty"`
	Extensions       []string       `json:"extensions,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	Kind             string         `json:"kind,omitempty"` // discriminator: "message"
}

// PartKind discriminates the variant of a Part.
type PartKind string

const (
	PartKindText PartKind = "text"
	PartKindFile PartKind = "file"
	PartKindData PartKind = "data"
)

// Part is a single piece of content within a Message or Artifact. Exactly one
// of Text, File, or Data is meaningful, selected by Kind. Custom JSON
// (un)marshaling maps this to the A2A discriminated-union wire shape, where the
// payload field present depends on "kind".
type Part struct {
	Kind     PartKind       `json:"kind"`
	Text     string         `json:"-"` // set when Kind == PartKindText
	File     *FileContent   `json:"-"` // set when Kind == PartKindFile
	Data     any            `json:"-"` // set when Kind == PartKindData
	Metadata map[string]any `json:"-"`
}

// FileContent is the payload of a file Part. Exactly one of Bytes or URI is set:
// Bytes for inline base64 content, URI for a reference to remote content.
type FileContent struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"` // base64-encoded inline content
	URI      string `json:"uri,omitempty"`   // reference to remote content
}

// MarshalJSON renders the Part in its discriminated-union wire form.
func (p Part) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case PartKindText:
		return json.Marshal(struct {
			Kind     PartKind       `json:"kind"`
			Text     string         `json:"text"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{p.Kind, p.Text, p.Metadata})
	case PartKindFile:
		return json.Marshal(struct {
			Kind     PartKind       `json:"kind"`
			File     *FileContent   `json:"file"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{p.Kind, p.File, p.Metadata})
	case PartKindData:
		return json.Marshal(struct {
			Kind     PartKind       `json:"kind"`
			Data     any            `json:"data"`
			Metadata map[string]any `json:"metadata,omitempty"`
		}{p.Kind, p.Data, p.Metadata})
	default:
		return nil, fmt.Errorf("a2a: cannot marshal part with unknown kind %q", p.Kind)
	}
}

// UnmarshalJSON parses a Part from its discriminated-union wire form.
func (p *Part) UnmarshalJSON(b []byte) error {
	var probe struct {
		Kind     PartKind        `json:"kind"`
		Text     string          `json:"text"`
		File     *FileContent    `json:"file"`
		Data     json.RawMessage `json:"data"`
		Metadata map[string]any  `json:"metadata"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	p.Kind = probe.Kind
	p.Metadata = probe.Metadata
	switch probe.Kind {
	case PartKindText:
		p.Text = probe.Text
	case PartKindFile:
		p.File = probe.File
	case PartKindData:
		if len(probe.Data) > 0 {
			var v any
			if err := json.Unmarshal(probe.Data, &v); err != nil {
				return err
			}
			p.Data = v
		}
	default:
		return fmt.Errorf("a2a: cannot unmarshal part with unknown kind %q", probe.Kind)
	}
	return nil
}

// NewTextPart builds a text Part.
func NewTextPart(text string) Part { return Part{Kind: PartKindText, Text: text} }

// NewDataPart builds a structured-data Part.
func NewDataPart(data any) Part { return Part{Kind: PartKindData, Data: data} }

// NewFilePart builds a file Part.
func NewFilePart(f *FileContent) Part { return Part{Kind: PartKindFile, File: f} }
