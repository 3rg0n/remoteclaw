package security

import (
	"sync"
	"time"
)

const (
	// challengeTTL is how long a pending challenge remains valid.
	challengeTTL = 2 * time.Minute
)

// PendingChallenge holds the details of a command awaiting confirmation.
type PendingChallenge struct {
	Command   string
	Reason    string
	CreatedAt time.Time
}

// ChallengeStore manages pending challenge-response confirmations for destructive commands.
// When a dangerous command is detected and a challenge string is configured, the store
// holds the command until the user confirms by sending the exact challenge string.
type ChallengeStore struct {
	challenge string // the challenge string users must send to confirm
	mu        sync.Mutex
	pending   map[string]*PendingChallenge // keyed by spaceID
}

// NewChallengeStore creates a challenge store with the given challenge string.
// If challenge is empty, challenge-response is disabled and Check always returns false.
func NewChallengeStore(challenge string) *ChallengeStore {
	cs := &ChallengeStore{
		challenge: challenge,
		pending:   make(map[string]*PendingChallenge),
	}
	if challenge != "" {
		go cs.cleanupLoop()
	}
	return cs
}

// Enabled returns true if challenge-response is configured.
func (cs *ChallengeStore) Enabled() bool {
	return cs.challenge != ""
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

// CheckResponse checks if the incoming text is the challenge string and if there is
// a pending command for this space. Returns the pending challenge and true if the
// response matches and the challenge hasn't expired. The pending entry is removed.
func (cs *ChallengeStore) CheckResponse(spaceID, text string) (*PendingChallenge, bool) {
	if cs.challenge == "" || text != cs.challenge {
		return nil, false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	pc, ok := cs.pending[spaceID]
	if !ok {
		return nil, false
	}

	// Check TTL
	if time.Since(pc.CreatedAt) > challengeTTL {
		delete(cs.pending, spaceID)
		return nil, false
	}

	delete(cs.pending, spaceID)
	return pc, true
}

// ClearPending removes any pending challenge for the given space.
func (cs *ChallengeStore) ClearPending(spaceID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.pending, spaceID)
}

// cleanupLoop removes expired pending challenges.
func (cs *ChallengeStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cs.mu.Lock()
		for id, pc := range cs.pending {
			if time.Since(pc.CreatedAt) > challengeTTL {
				delete(cs.pending, id)
			}
		}
		cs.mu.Unlock()
	}
}
