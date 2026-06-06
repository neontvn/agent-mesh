// Package a2a defines Go types for the Agent2Agent (A2A) protocol wire format,
// in its JSON-RPC 2.0 representation.
//
// Field names and enum values follow the A2A JSON representation (camelCase
// field names, hyphenated lowercase enum strings) rather than the gRPC/proto
// representation, because AgentMesh's sidecar-to-sidecar transport is
// HTTPS + JSON-RPC 2.0.
//
// The pinned spec version is recorded in internal/a2a/SPEC_VERSION (v1.0.1).
// Where a field name or method string is not yet verified against the official
// a2aproject/a2a-go SDK, it is marked with a TODO.
package a2a
