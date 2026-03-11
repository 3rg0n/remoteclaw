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

func TestEncryptChallenge(t *testing.T) {
	passphrase := "my-secret-key-2026" //nolint:gosec // test data

	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)
	assert.NotEmpty(t, encrypted)

	// Same passphrase should verify
	assert.True(t, verifyPassphrase(encrypted, passphrase))

	// Wrong passphrase should fail
	assert.False(t, verifyPassphrase(encrypted, "wrong-key"))

	// Empty passphrase should fail
	assert.False(t, verifyPassphrase(encrypted, ""))

	// Corrupt ciphertext should fail
	assert.False(t, verifyPassphrase("not-valid-base64!!!", passphrase))
}

func TestEncryptChallenge_EmptyPassphrase(t *testing.T) {
	_, err := EncryptChallenge("")
	assert.Error(t, err)
}

func TestEncryptChallenge_DifferentEncryptions(t *testing.T) {
	passphrase := "same-key"

	enc1, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	enc2, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	// Different salt + nonce means different ciphertexts
	assert.NotEqual(t, enc1, enc2)

	// But both should verify with the same passphrase
	assert.True(t, verifyPassphrase(enc1, passphrase))
	assert.True(t, verifyPassphrase(enc2, passphrase))
}

func TestChallengeStore_MatchingChallenge(t *testing.T) {
	passphrase := "confirm-delete-123"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	assert.True(t, cs.Enabled())

	cs.SetPending("space-1", "rm -rf /tmp/old", "recursive deletion")

	// Wrong passphrase
	_, ok := cs.CheckResponse("space-1", "wrong-string")
	assert.False(t, ok)

	// Correct passphrase (decryption key)
	pc, ok := cs.CheckResponse("space-1", passphrase)
	require.True(t, ok)
	assert.Equal(t, "rm -rf /tmp/old", pc.Command)
	assert.Equal(t, "recursive deletion", pc.Reason)

	// Second check should fail (consumed)
	_, ok = cs.CheckResponse("space-1", passphrase)
	assert.False(t, ok, "challenge should be consumed after first match")
}

func TestChallengeStore_WrongSpace(t *testing.T) {
	passphrase := "confirm"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	cs.SetPending("space-1", "shutdown", "system shutdown")

	// Right passphrase, wrong space
	_, ok := cs.CheckResponse("space-2", passphrase)
	assert.False(t, ok)

	// Right passphrase, right space
	pc, ok := cs.CheckResponse("space-1", passphrase)
	require.True(t, ok)
	assert.Equal(t, "shutdown", pc.Command)
}

func TestChallengeStore_Expiration(t *testing.T) {
	passphrase := "confirm"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	cs.SetPending("space-1", "reboot", "system reboot")

	// Manually expire the challenge
	cs.mu.Lock()
	cs.pending["space-1"].CreatedAt = time.Now().Add(-3 * time.Minute)
	cs.mu.Unlock()

	_, ok := cs.CheckResponse("space-1", passphrase)
	assert.False(t, ok, "expired challenge should not match")
}

func TestChallengeStore_Overwrite(t *testing.T) {
	passphrase := "confirm"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	cs.SetPending("space-1", "command-1", "reason-1")
	cs.SetPending("space-1", "command-2", "reason-2")

	pc, ok := cs.CheckResponse("space-1", passphrase)
	require.True(t, ok)
	assert.Equal(t, "command-2", pc.Command, "should return the most recent pending command")
}

func TestChallengeStore_ClearPending(t *testing.T) {
	passphrase := "confirm"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	cs.SetPending("space-1", "shutdown", "system shutdown")
	cs.ClearPending("space-1")

	_, ok := cs.CheckResponse("space-1", passphrase)
	assert.False(t, ok, "cleared challenge should not match")
}

func TestChallengeStore_ReplayResistance(t *testing.T) {
	passphrase := "my-secret"
	encrypted, err := EncryptChallenge(passphrase)
	require.NoError(t, err)

	cs := NewChallengeStore(encrypted)
	cs.SetPending("space-1", "dangerous-cmd", "danger")

	// Sending the encrypted ciphertext itself should NOT work as the response
	_, ok := cs.CheckResponse("space-1", encrypted)
	assert.False(t, ok, "sending the ciphertext value should not authenticate")
}
