package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChallengeStore_Disabled(t *testing.T) {
	cs := NewChallengeStore("")
	assert.False(t, cs.Enabled())

	cs.SetPending("space-1", "rm -rf /", "destructive")
	_, ok := cs.CheckResponse("space-1", "")
	assert.False(t, ok, "should not match when challenge is empty")
}

func TestChallengeStore_MatchingChallenge(t *testing.T) {
	cs := NewChallengeStore("confirm-delete-123")
	assert.True(t, cs.Enabled())

	cs.SetPending("space-1", "rm -rf /tmp/old", "recursive deletion")

	// Wrong challenge string
	_, ok := cs.CheckResponse("space-1", "wrong-string")
	assert.False(t, ok)

	// Correct challenge string
	pc, ok := cs.CheckResponse("space-1", "confirm-delete-123")
	require.True(t, ok)
	assert.Equal(t, "rm -rf /tmp/old", pc.Command)
	assert.Equal(t, "recursive deletion", pc.Reason)

	// Second check should fail (consumed)
	_, ok = cs.CheckResponse("space-1", "confirm-delete-123")
	assert.False(t, ok, "challenge should be consumed after first match")
}

func TestChallengeStore_WrongSpace(t *testing.T) {
	cs := NewChallengeStore("confirm")

	cs.SetPending("space-1", "shutdown", "system shutdown")

	// Right challenge, wrong space
	_, ok := cs.CheckResponse("space-2", "confirm")
	assert.False(t, ok)

	// Right challenge, right space
	pc, ok := cs.CheckResponse("space-1", "confirm")
	require.True(t, ok)
	assert.Equal(t, "shutdown", pc.Command)
}

func TestChallengeStore_Expiration(t *testing.T) {
	cs := NewChallengeStore("confirm")

	cs.SetPending("space-1", "reboot", "system reboot")

	// Manually expire the challenge
	cs.mu.Lock()
	cs.pending["space-1"].CreatedAt = time.Now().Add(-3 * time.Minute)
	cs.mu.Unlock()

	_, ok := cs.CheckResponse("space-1", "confirm")
	assert.False(t, ok, "expired challenge should not match")
}

func TestChallengeStore_Overwrite(t *testing.T) {
	cs := NewChallengeStore("confirm")

	cs.SetPending("space-1", "command-1", "reason-1")
	cs.SetPending("space-1", "command-2", "reason-2")

	pc, ok := cs.CheckResponse("space-1", "confirm")
	require.True(t, ok)
	assert.Equal(t, "command-2", pc.Command, "should return the most recent pending command")
}

func TestChallengeStore_ClearPending(t *testing.T) {
	cs := NewChallengeStore("confirm")

	cs.SetPending("space-1", "shutdown", "system shutdown")
	cs.ClearPending("space-1")

	_, ok := cs.CheckResponse("space-1", "confirm")
	assert.False(t, ok, "cleared challenge should not match")
}
