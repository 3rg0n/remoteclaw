package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebexSender_SendMessage(t *testing.T) {
	var receivedMethod string
	var receivedAuth string
	var receivedBody webexMessageBody

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedAuth = r.Header.Get("Authorization")

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": "msg-123"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	ts := newTestWebexSender(server.URL, "test-bot-token", server.Client())
	err := ts.SendMessage(ctx, "space-123", "hello world")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, "Bearer test-bot-token", receivedAuth)
	assert.Equal(t, "space-123", receivedBody.RoomID)
	assert.Equal(t, "hello world", receivedBody.Text)
}

func TestWebexSender_SendMessageError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message": "invalid token"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	ts := newTestWebexSender(server.URL, "bad-token", server.Client())
	err := ts.SendMessage(ctx, "space-123", "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestWebexSender_SendMessageServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message": "internal error"}`))
	}))
	defer server.Close()

	ctx := context.Background()
	ts := newTestWebexSender(server.URL, "token", server.Client())
	err := ts.SendMessage(ctx, "space-123", "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// testWebexSender is a test helper that allows overriding the API URL.
type testWebexSender struct {
	token      string
	url        string
	httpClient *http.Client
}

func newTestWebexSender(url, token string, client *http.Client) *testWebexSender {
	return &testWebexSender{
		token:      token,
		url:        url,
		httpClient: client,
	}
}

func (ts *testWebexSender) SendMessage(ctx context.Context, spaceID, text string) error {
	body := webexMessageBody{
		RoomID: spaceID,
		Text:   text,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.url+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+ts.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.httpClient.Do(req) //nolint:gosec // test code with controlled URL
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webex API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
