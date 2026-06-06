package a2a

// AgentCard is an agent's self-description. In A2A it is served at
// /.well-known/agent-card.json (older drafts used /.well-known/agent.json).
type AgentCard struct {
	ProtocolVersion    string            `json:"protocolVersion,omitempty"`
	Name               string            `json:"name"`
	Description        string            `json:"description"`
	URL                string            `json:"url"`
	PreferredTransport string            `json:"preferredTransport,omitempty"`
	Version            string            `json:"version"`
	Provider           *AgentProvider    `json:"provider,omitempty"`
	Capabilities       AgentCapabilities `json:"capabilities"`
	DefaultInputModes  []string          `json:"defaultInputModes,omitempty"`
	DefaultOutputModes []string          `json:"defaultOutputModes,omitempty"`
	Skills             []AgentSkill      `json:"skills"`

	AdditionalInterfaces []AgentInterface `json:"additionalInterfaces,omitempty"`

	// SecuritySchemes and Security describe authentication requirements. They
	// are out of scope for the v1 retrofit (see DESIGN.md §5.4) but modeled so
	// the card parses cards that declare them.
	SecuritySchemes map[string]any        `json:"securitySchemes,omitempty"`
	Security        []map[string][]string `json:"security,omitempty"`

	SupportsAuthenticatedExtendedCard bool `json:"supportsAuthenticatedExtendedCard,omitempty"`
}

// AgentProvider identifies the organization that publishes an agent.
type AgentProvider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// AgentCapabilities declares optional protocol features an agent supports.
type AgentCapabilities struct {
	Streaming              bool     `json:"streaming,omitempty"`
	PushNotifications      bool     `json:"pushNotifications,omitempty"`
	StateTransitionHistory bool     `json:"stateTransitionHistory,omitempty"`
	Extensions             []string `json:"extensions,omitempty"`
}

// AgentSkill is a single capability the agent advertises. ID is the stable
// machine identifier; Name/Description are human-facing.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentInterface is an additional (transport, url) endpoint the agent exposes,
// beyond the primary URL on the card.
type AgentInterface struct {
	URL       string `json:"url"`
	Transport string `json:"transport"`
}
