package a2a

// A2A JSON-RPC method names.
const (
	MethodMessageSend      = "message/send"
	MethodMessageStream    = "message/stream"
	MethodTasksGet         = "tasks/get"
	MethodTasksCancel      = "tasks/cancel"
	MethodTasksResubscribe = "tasks/resubscribe"

	MethodPushConfigSet = "tasks/pushNotificationConfig/set"
	MethodPushConfigGet = "tasks/pushNotificationConfig/get"

	MethodAuthenticatedExtendedCard = "agent/getAuthenticatedExtendedCard"
)
