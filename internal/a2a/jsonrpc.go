package a2a

import "encoding/json"

// JSONRPCVersion is the fixed protocol version string carried by every A2A message
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request object. A2A methods are invoked by sending
// one of these to an agent's A2A endpoint. ID is omitted for notifications
type Request struct {
	JSONRPC string          `json:"jsonrpc"`          // always "2.0"
	ID      any             `json:"id,omitempty"`     // string or number
	Method  string          `json:"method"`           // e.g. "message/send"
	Params  json.RawMessage `json:"params,omitempty"` // method-specific params
}

// Response is a JSON-RPC 2.0 response object. Exactly one of Result or Error
// is set on a well-formed response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC 2.0 error object. It implements the error interface so it
// can flow through normal Go error handling
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Standard JSON-RPC 2.0 error codes
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// A2A-specific error codes live in the JSON-RPC server-error range
// (-32000 to -32099)
const (
	CodeTaskNotFound                 = -32001
	CodeTaskNotCancelable            = -32002
	CodePushNotificationsUnsupported = -32003
	CodeUnsupportedOperation         = -32004
	CodeContentTypeNotSupported      = -32005

	// Standard A2A-specific errors also defined by the spec
	CodeInvalidAgentResponse           = -32006
	CodeExtendedAgentCardNotConfigured = -32007
	CodeExtensionSupportRequired       = -32008
	CodeVersionNotSupported            = -32009
)
