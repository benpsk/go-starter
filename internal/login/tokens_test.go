package login

import (
	"context"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
)

func TestIssueTokenPairMintsUsableAccessToken(t *testing.T) {
	svc, repo := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()

	pair, err := svc.IssueTokenPair(context.Background(), 42, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.TokenType != "bearer" || pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("unexpected token pair: %+v", pair)
	}
	if !pair.AccessTokenExpiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("unexpected access expiry: %v", pair.AccessTokenExpiresAt)
	}
	if !pair.RefreshTokenExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("unexpected refresh expiry: %v", pair.RefreshTokenExpiresAt)
	}

	// Only the hash is persisted, never the token itself.
	if _, ok := repo.tokens[HashToken(pair.RefreshToken)]; !ok {
		t.Fatalf("expected the refresh token hash to be stored")
	}
	if _, ok := repo.tokens[pair.RefreshToken]; ok {
		t.Fatalf("the raw refresh token must never be stored")
	}

	claims, err := svc.ParseAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("the issued access token should parse: %v", err)
	}
	if claims.UserID != 42 || claims.FamilyID == "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestIssueTokenPairRequiresConfiguredSecret(t *testing.T) {
	cfg := testConfig()
	cfg.AccessTokenSecret = ""
	svc, _ := newTestService(t, cfg, &verifierStub{}, &userServiceStub{})

	if _, err := svc.IssueTokenPair(context.Background(), 1, time.Now()); kindOf(t, err) != apperror.UnavailableDependency {
		t.Fatalf("expected an unavailable dependency, got %v", err)
	}
}

func TestRotateRefreshTokenIssuesNewPair(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()

	first, err := svc.IssueTokenPair(context.Background(), 7, now)
	if err != nil {
		t.Fatalf("unexpected issue error: %v", err)
	}

	second, err := svc.RotateRefreshToken(context.Background(), first.RefreshToken, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("unexpected rotate error: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatalf("rotation must return a different refresh token")
	}

	claims, err := svc.ParseAccessToken(second.AccessToken)
	if err != nil {
		t.Fatalf("the rotated access token should parse: %v", err)
	}
	if claims.UserID != 7 {
		t.Fatalf("rotation must preserve the subject, got %+v", claims)
	}
}

func TestRotateRefreshTokenReuseRevokesFamily(t *testing.T) {
	svc, repo := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()

	first, err := svc.IssueTokenPair(context.Background(), 7, now)
	if err != nil {
		t.Fatalf("unexpected issue error: %v", err)
	}
	if _, err := svc.RotateRefreshToken(context.Background(), first.RefreshToken, now); err != nil {
		t.Fatalf("unexpected rotate error: %v", err)
	}

	// Replaying the already-rotated token means it leaked, so the whole family
	// must be revoked rather than just the replayed token.
	if _, err := svc.RotateRefreshToken(context.Background(), first.RefreshToken, now); kindOf(t, err) != apperror.Unauthenticated {
		t.Fatalf("expected an unauthenticated error on reuse, got %v", err)
	}
	if len(repo.familyRevoked) != 1 {
		t.Fatalf("expected the token family to be revoked once, got %v", repo.familyRevoked)
	}
}

func TestRotateRefreshTokenRejectsUnknownAndEmpty(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	cases := []struct {
		name     string
		token    string
		wantKind apperror.Kind
	}{
		{"empty", "", apperror.Invalid},
		{"unknown", "not-a-token", apperror.Unauthenticated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RotateRefreshToken(context.Background(), tc.token, time.Now())
			if got := kindOf(t, err); got != tc.wantKind {
				t.Fatalf("expected %s, got %s (%v)", tc.wantKind, got, err)
			}
		})
	}
}

func TestRevokeRefreshTokenIsIdempotent(t *testing.T) {
	svc, repo := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()
	pair, _ := svc.IssueTokenPair(context.Background(), 5, now)

	if err := svc.RevokeRefreshToken(context.Background(), pair.RefreshToken, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.RevokeRefreshToken(context.Background(), "", now); err != nil {
		t.Fatalf("revoking nothing must not fail: %v", err)
	}
	if len(repo.revoked) != 1 {
		t.Fatalf("expected exactly one revocation, got %v", repo.revoked)
	}

	if _, err := svc.RotateRefreshToken(context.Background(), pair.RefreshToken, now); kindOf(t, err) != apperror.Unauthenticated {
		t.Fatalf("a revoked token must not rotate, got %v", err)
	}
}

func TestParseAccessTokenRejectsBadTokens(t *testing.T) {
	cfg := testConfig()
	svc, _ := newTestService(t, cfg, &verifierStub{}, &userServiceStub{})
	now := time.Now()
	valid, _ := svc.IssueTokenPair(context.Background(), 9, now)

	otherCfg := cfg
	otherCfg.AccessTokenSecret = "another-secret"
	otherSvc, _ := newTestService(t, otherCfg, &verifierStub{}, &userServiceStub{})
	foreign, _ := otherSvc.IssueTokenPair(context.Background(), 9, now)

	expiredCfg := cfg
	expiredCfg.AccessTokenTTL = time.Nanosecond
	expiredSvc, _ := newTestService(t, expiredCfg, &verifierStub{}, &userServiceStub{})
	expired, _ := expiredSvc.IssueTokenPair(context.Background(), 9, now.Add(-time.Hour))

	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"valid", valid.AccessToken, false},
		{"empty", "", true},
		{"garbage", "not.a.jwt", true},
		{"signed with another secret", foreign.AccessToken, true},
		{"expired", expired.AccessToken, true},
		{"refresh token is not an access token", valid.RefreshToken, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ParseAccessToken(tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseAccessToken error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && kindOf(t, err) != apperror.Unauthenticated {
				t.Fatalf("expected an unauthenticated error, got %v", err)
			}
		})
	}
}

func TestHashTokenIsStableAndTrims(t *testing.T) {
	if HashToken("abc") != HashToken(" abc ") {
		t.Fatalf("expected surrounding whitespace to be ignored")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatalf("expected different inputs to hash differently")
	}
	if len(HashToken("abc")) != 64 {
		t.Fatalf("expected a hex sha256 digest, got %q", HashToken("abc"))
	}
}

// compile-time assertion that the stub satisfies the domain interface
var _ UserResolver = (*userServiceStub)(nil)

func TestPruneRefreshTokens(t *testing.T) {
	svc, repo := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()

	live, err := svc.IssueTokenPair(context.Background(), 1, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stale, err := svc.IssueTokenPair(context.Background(), 2, now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	removed, err := svc.PruneRefreshTokens(context.Background(), 30*24*time.Hour, now)
	if err != nil {
		t.Fatalf("PruneRefreshTokens failed: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 token removed, got %d", removed)
	}
	if _, ok := repo.tokens[HashToken(stale.RefreshToken)]; ok {
		t.Fatalf("expected the stale token to be gone")
	}
	if _, ok := repo.tokens[HashToken(live.RefreshToken)]; !ok {
		t.Fatalf("expected the live token to survive")
	}
	if want := now.Add(-30 * 24 * time.Hour); !repo.pruneCutoff.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", repo.pruneCutoff, want)
	}
}

func TestPruneRefreshTokensRejectsANegativeGrace(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	if _, err := svc.PruneRefreshTokens(context.Background(), -time.Hour, time.Now()); kindOf(t, err) != apperror.Invalid {
		t.Fatalf("expected invalid input, got %v", err)
	}
}

func TestPruneRefreshTokensAllowsAZeroGrace(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})
	now := time.Now()

	if _, err := svc.IssueTokenPair(context.Background(), 1, now); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A zero grace means "everything already expired or revoked", which must
	// not touch a live token.
	removed, err := svc.PruneRefreshTokens(context.Background(), 0, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected no live token to be removed, got %d", removed)
	}
}
