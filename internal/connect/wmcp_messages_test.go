package connect

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWMCPEnvelope_MarshalAuth(t *testing.T) {
	env := WMCPEnvelope{
		Type:  "auth",
		Token: "my-token",
	}

	data, err := json.Marshal(env)
	require.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "auth", decoded["type"])
	assert.Equal(t, "my-token", decoded["token"])
	// Omitempty fields should not be present
	_, hasRequestID := decoded["request_id"]
	assert.False(t, hasRequestID)
}

func TestWMCPEnvelope_MarshalResponse(t *testing.T) {
	env := WMCPEnvelope{
		Type:      "response",
		RequestID: "req-123",
		SpaceID:   "space-456",
		Text:      "hello world",
	}

	data, err := json.Marshal(env)
	require.NoError(t, err)

	var decoded WMCPEnvelope
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "response", decoded.Type)
	assert.Equal(t, "req-123", decoded.RequestID)
	assert.Equal(t, "space-456", decoded.SpaceID)
	assert.Equal(t, "hello world", decoded.Text)
}

func TestWMCPEnvelope_UnmarshalMessage(t *testing.T) {
	raw := `{
		"type": "message",
		"request_id": "uuid-abc",
		"space_id": "space-1",
		"person_id": "person-1",
		"email": "user@example.com",
		"text": "list processes"
	}`

	var env WMCPEnvelope
	err := json.Unmarshal([]byte(raw), &env)
	require.NoError(t, err)

	assert.Equal(t, "message", env.Type)
	assert.Equal(t, "uuid-abc", env.RequestID)
	assert.Equal(t, "space-1", env.SpaceID)
	assert.Equal(t, "person-1", env.PersonID)
	assert.Equal(t, "user@example.com", env.Email)
	assert.Equal(t, "list processes", env.Text)
}

func TestWMCPEnvelope_UnmarshalAuthOk(t *testing.T) {
	raw := `{"type": "auth_ok"}`
	var env WMCPEnvelope
	err := json.Unmarshal([]byte(raw), &env)
	require.NoError(t, err)
	assert.Equal(t, "auth_ok", env.Type)
}

func TestWMCPEnvelope_UnmarshalAuthError(t *testing.T) {
	raw := `{"type": "auth_error", "error": "invalid token"}`
	var env WMCPEnvelope
	err := json.Unmarshal([]byte(raw), &env)
	require.NoError(t, err)
	assert.Equal(t, "auth_error", env.Type)
	assert.Equal(t, "invalid token", env.Error)
}

func TestWMCPEnvelope_MarshalHeartbeat(t *testing.T) {
	env := WMCPEnvelope{Type: "heartbeat"}
	data, err := json.Marshal(env)
	require.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "heartbeat", decoded["type"])
	// Only type should be present
	assert.Len(t, decoded, 1)
}

func TestWMCPEnvelope_UnmarshalHeartbeatAck(t *testing.T) {
	raw := `{"type": "heartbeat_ack"}`
	var env WMCPEnvelope
	err := json.Unmarshal([]byte(raw), &env)
	require.NoError(t, err)
	assert.Equal(t, "heartbeat_ack", env.Type)
}
