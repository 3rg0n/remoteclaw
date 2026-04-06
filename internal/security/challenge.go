package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	// challengeTTL is how long a pending challenge remains valid.
	challengeTTL = 2 * time.Minute

	// challengeSentinel is the known plaintext encrypted during setup.
	// Successful decryption to this value proves the user knows the key.
	challengeSentinel = "REMOTECLAW_CHALLENGE_OK"

	// scrypt parameters (N=32768, r=8, p=1) — ~100ms on modern hardware
	scryptN = 32768
	scryptR = 8
	scryptP = 1
	keyLen  = 32 // AES-256
	saltLen = 16
)

// PendingChallenge holds the details of a command awaiting confirmation.
type PendingChallenge struct {
	Command   string
	Reason    string
	CreatedAt time.Time
}

// maxChallengeAttempts is the maximum number of failed passphrase attempts per space
// before the pending challenge is revoked.
const maxChallengeAttempts = 3

// ChallengeStore manages pending challenge-response confirmations for destructive commands.
// The challenge value is an AES-256-GCM encrypted sentinel. When a user responds with
// the correct passphrase, the system derives the AES key via scrypt and attempts to
// decrypt. GCM authentication failure = wrong key, successful decryption = confirmed.
type ChallengeStore struct {
	ciphertext string // base64-encoded AES-256-GCM ciphertext (salt + nonce + encrypted)
	mu         sync.Mutex
	pending    map[string]*PendingChallenge // keyed by spaceID
	attempts   map[string]int               // failed attempt count per spaceID
	stopCh     chan struct{}                 // signals cleanup goroutine to stop
}

// NewChallengeStore creates a challenge store with the given encrypted challenge value.
// The value should be produced by EncryptChallenge. If empty, challenge-response is disabled.
func NewChallengeStore(encryptedChallenge string) *ChallengeStore {
	cs := &ChallengeStore{
		ciphertext: encryptedChallenge,
		pending:    make(map[string]*PendingChallenge),
		attempts:   make(map[string]int),
		stopCh:     make(chan struct{}),
	}
	if encryptedChallenge != "" {
		go cs.cleanupLoop()
	}
	return cs
}

// Enabled returns true if challenge-response is configured.
func (cs *ChallengeStore) Enabled() bool {
	return cs.ciphertext != ""
}

// SetPending stores a pending challenge for the given space.
// Overwrites any existing pending challenge for that space.
func (cs *ChallengeStore) SetPending(spaceID, command, reason string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.pending[spaceID] = &PendingChallenge{
		Command:   command,
		Reason:    reason,
		CreatedAt: time.Now(),
	}
}

// CheckResponse attempts to decrypt the stored ciphertext using the provided text
// as the passphrase. If decryption succeeds and the plaintext matches the sentinel,
// and there is a non-expired pending command for this space, returns the pending
// challenge. The pending entry is removed on success.
// Enforces a brute-force limit: after maxChallengeAttempts failed attempts, the
// pending challenge for that space is revoked.
func (cs *ChallengeStore) CheckResponse(spaceID, text string) (*PendingChallenge, bool) {
	if cs.ciphertext == "" || text == "" {
		return nil, false
	}

	cs.mu.Lock()
	// Check if there is a pending challenge before doing expensive scrypt work
	pc, ok := cs.pending[spaceID]
	if !ok {
		cs.mu.Unlock()
		return nil, false
	}
	// Check TTL
	if time.Since(pc.CreatedAt) > challengeTTL {
		delete(cs.pending, spaceID)
		delete(cs.attempts, spaceID)
		cs.mu.Unlock()
		return nil, false
	}
	// Check brute-force limit
	if cs.attempts[spaceID] >= maxChallengeAttempts {
		delete(cs.pending, spaceID)
		delete(cs.attempts, spaceID)
		cs.mu.Unlock()
		return nil, false
	}
	cs.mu.Unlock()

	// Try to decrypt — this is the authentication step (expensive scrypt)
	if !verifyPassphrase(cs.ciphertext, text) {
		cs.mu.Lock()
		cs.attempts[spaceID]++
		if cs.attempts[spaceID] >= maxChallengeAttempts {
			// Revoke the pending challenge after too many failed attempts
			delete(cs.pending, spaceID)
			delete(cs.attempts, spaceID)
		}
		cs.mu.Unlock()
		return nil, false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Re-check pending (may have been cleaned up concurrently)
	pc, ok = cs.pending[spaceID]
	if !ok {
		return nil, false
	}
	if time.Since(pc.CreatedAt) > challengeTTL {
		delete(cs.pending, spaceID)
		delete(cs.attempts, spaceID)
		return nil, false
	}

	delete(cs.pending, spaceID)
	delete(cs.attempts, spaceID)
	return pc, true
}

// ClearPending removes any pending challenge for the given space.
func (cs *ChallengeStore) ClearPending(spaceID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.pending, spaceID)
}

// Close stops the background cleanup goroutine.
func (cs *ChallengeStore) Close() {
	select {
	case <-cs.stopCh:
		// already closed
	default:
		close(cs.stopCh)
	}
}

// cleanupLoop removes expired pending challenges.
func (cs *ChallengeStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			cs.mu.Lock()
			for id, pc := range cs.pending {
				if time.Since(pc.CreatedAt) > challengeTTL {
					delete(cs.pending, id)
				}
			}
			cs.mu.Unlock()
		}
	}
}

// EncryptChallenge encrypts the sentinel value with the given passphrase using
// AES-256-GCM with a scrypt-derived key. Returns base64-encoded (salt + nonce + ciphertext).
func EncryptChallenge(passphrase string) (string, error) {
	if passphrase == "" {
		return "", errors.New("passphrase cannot be empty")
	}

	// Generate random salt
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}

	// Derive key from passphrase
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return "", fmt.Errorf("deriving key: %w", err)
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, []byte(challengeSentinel), nil)

	// Combine: salt + nonce + ciphertext
	combined := make([]byte, 0, saltLen+len(nonce)+len(ciphertext))
	combined = append(combined, salt...)
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)

	return base64.StdEncoding.EncodeToString(combined), nil
}

// verifyPassphrase attempts to decrypt the base64 ciphertext with the given passphrase.
// Returns true if decryption succeeds and produces the expected sentinel.
func verifyPassphrase(encoded, passphrase string) bool {
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}

	// Extract salt
	if len(combined) < saltLen {
		return false
	}
	salt := combined[:saltLen]
	rest := combined[saltLen:]

	// Derive key
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return false
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return false
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return false
	}

	// Extract nonce
	nonceSize := gcm.NonceSize()
	if len(rest) < nonceSize {
		return false
	}
	nonce := rest[:nonceSize]
	ciphertext := rest[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return false // GCM authentication failed — wrong key
	}

	return string(plaintext) == challengeSentinel
}
