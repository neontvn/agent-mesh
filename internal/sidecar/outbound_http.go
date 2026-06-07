package sidecar

import (
	"encoding/json"
	"net/http"
)

// meshInvokeRequest is the body of POST /mesh/invoke — the local API a running
// agent uses to call other agents through the mesh. Input is opaque JSON passed
// through to the target agent unchanged.
type meshInvokeRequest struct {
	Capability string            `json:"capability"`
	Input      json.RawMessage   `json:"input"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// meshAPIHandler exposes the sidecar's outbound API on loopback. The agent POSTs
// {capability, input} and gets back the target agent's result bytes. All routing,
// circuit breaking, and reporting happen in the Caller.
func meshAPIHandler(caller *Caller) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mesh/invoke", func(w http.ResponseWriter, r *http.Request) {
		var req meshInvokeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Capability == "" {
			http.Error(w, "capability is required", http.StatusBadRequest)
			return
		}

		out, err := caller.Invoke(r.Context(), req.Capability, []byte(req.Input), req.Metadata)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	})
	return mux
}
