package a2a

// Artifact is a typed output produced by an agent during a Task. Like a
// Message, its content lives in Parts, but an Artifact represents a deliverable
// result rather than a conversational turn.
type Artifact struct {
	ArtifactID  string         `json:"artifactId"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Extensions  []string       `json:"extensions,omitempty"`
}
