package connect

// WMCPEnvelope is the JSON message format used over the WMCP WebSocket protocol.
//
// Client → Server:
//
//	{ "type": "auth",      "token": "<agent-token>" }
//	{ "type": "response",  "request_id": "<id>", "space_id": "<id>", "text": "<response>" }
//	{ "type": "heartbeat" }
//
// Server → Client:
//
//	{ "type": "auth_ok" }
//	{ "type": "auth_error", "error": "<reason>" }
//	{ "type": "message",    "request_id": "<uuid>", "space_id": "<id>", "person_id": "<id>", "email": "<email>", "text": "<text>" }
//	{ "type": "heartbeat_ack" }
type WMCPEnvelope struct {
	Type      string `json:"type"`
	Token     string `json:"token,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	SpaceID   string `json:"space_id,omitempty"`
	PersonID  string `json:"person_id,omitempty"`
	Email     string `json:"email,omitempty"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}
