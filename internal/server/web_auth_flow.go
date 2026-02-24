package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var errOAuthFlowNotFound = errors.New("oauth flow not found")

type oauthFlowRecord struct {
	State        string
	Provider     string
	CodeVerifier string
	RedirectTo   string
	ExpiresAt    time.Time
}

type oauthFlowStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	flows map[string]oauthFlowRecord
}

func newOAuthFlowStore(ttl time.Duration) *oauthFlowStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &oauthFlowStore{ttl: ttl, flows: map[string]oauthFlowRecord{}}
}

func (s *oauthFlowStore) create(provider, redirectTo string, now time.Time) (oauthFlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)

	state, err := randomToken(24)
	if err != nil {
		return oauthFlowRecord{}, err
	}
	verifier, err := randomToken(32)
	if err != nil {
		return oauthFlowRecord{}, err
	}
	record := oauthFlowRecord{
		State:        state,
		Provider:     provider,
		CodeVerifier: verifier,
		RedirectTo:   redirectTo,
		ExpiresAt:    now.Add(s.ttl),
	}
	s.flows[state] = record
	return record, nil
}

func (s *oauthFlowStore) consume(state, provider string, now time.Time) (oauthFlowRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)

	record, ok := s.flows[state]
	if !ok {
		return oauthFlowRecord{}, errOAuthFlowNotFound
	}
	delete(s.flows, state)
	if record.Provider != provider || now.After(record.ExpiresAt) {
		return oauthFlowRecord{}, errOAuthFlowNotFound
	}
	return record, nil
}

func (s *oauthFlowStore) cleanupLocked(now time.Time) {
	for state, record := range s.flows {
		if now.After(record.ExpiresAt) {
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
