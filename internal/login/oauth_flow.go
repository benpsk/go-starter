package login

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var errFlowNotFound = errors.New("oauth flow not found")

// oauthFlow is one pending browser authorization round trip.
//
// Flows live in memory, which is correct for a single instance. Move this to
// shared storage (Redis, or a table) before running more than one replica.
type oauthFlow struct {
	State        string
	Provider     string
	CodeVerifier string
	RedirectTo   string
	ExpiresAt    time.Time
}

type oauthFlowStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	flows map[string]oauthFlow
}

func newOAuthFlowStore(ttl time.Duration) *oauthFlowStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &oauthFlowStore{ttl: ttl, flows: map[string]oauthFlow{}}
}

func (s *oauthFlowStore) create(provider, redirectTo string, now time.Time) (oauthFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)

	state, err := randomToken(24)
	if err != nil {
		return oauthFlow{}, err
	}
	verifier, err := randomToken(32)
	if err != nil {
		return oauthFlow{}, err
	}
	flow := oauthFlow{
		State:        state,
		Provider:     provider,
		CodeVerifier: verifier,
		RedirectTo:   redirectTo,
		ExpiresAt:    now.Add(s.ttl),
	}
	s.flows[state] = flow
	return flow, nil
}

func (s *oauthFlowStore) consume(state, provider string, now time.Time) (oauthFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)

	flow, ok := s.flows[state]
	if !ok {
		return oauthFlow{}, errFlowNotFound
	}
	delete(s.flows, state)
	if flow.Provider != provider || now.After(flow.ExpiresAt) {
		return oauthFlow{}, errFlowNotFound
	}
	return flow, nil
}

func (s *oauthFlowStore) cleanupLocked(now time.Time) {
	for state, flow := range s.flows {
		if now.After(flow.ExpiresAt) {
			delete(s.flows, state)
		}
	}
}

func randomToken(n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
