package a2adp

import "github.com/neontvn/agent-mesh/internal/a2a"

// BuildCard assembles an AgentCard from sidecar identity and capability flags.
//
// v1 derives one AgentSkill per capability string (id == name). Richer skill
// metadata (descriptions, examples, IO modes) could later come from the agent
// application declaring its own skills; this keeps the mapping in one place so
// that change is localized.
func BuildCard(name, description, url, version string, capabilities []string) a2a.AgentCard {
	skills := make([]a2a.AgentSkill, 0, len(capabilities))
	for _, c := range capabilities {
		skills = append(skills, a2a.AgentSkill{
			ID:          c,
			Name:        c,
			Description: "Capability " + c,
		})
	}
	return a2a.AgentCard{
		ProtocolVersion:    "1.0.1",
		Name:               name,
		Description:        description,
		URL:                url,
		PreferredTransport: "JSONRPC",
		Version:            version,
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Skills:             skills,
	}
}
