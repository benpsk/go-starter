package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/users"
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

	if _, err := svc.IssueTokenPair(context.Background(), 1, time.Now()); statusOf(t, err) != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %v", err)
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
	if _, err := svc.RotateRefreshToken(context.Background(), first.RefreshToken, now); statusOf(t, err) != http.StatusUnauthorized {
		t.Fatalf("expected 401 on reuse, got %v", err)
	}
	if len(repo.familyRevoked) != 1 {
		t.Fatalf("expected the token family to be revoked once, got %v", repo.familyRevoked)
	}
}

func TestRotateRefreshTokenRejectsUnknownAndEmpty(t *testing.T) {
	svc, _ := newTestService(t, testConfig(), &verifierStub{}, &userServiceStub{})

	cases := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"empty", "", http.StatusBadRequest},
		{"unknown", "not-a-token", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RotateRefreshToken(context.Background(), tc.token, time.Now())
			if got := statusOf(t, err); got != tc.wantStatus {
				t.Fatalf("expected %d, got %d (%v)", tc.wantStatus, got, err)
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

	if _, err := svc.RotateRefreshToken(context.Background(), pair.RefreshToken, now); statusOf(t, err) != http.StatusUnauthorized {
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
			if err != nil && statusOf(t, err) != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %v", err)
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

func TestBearerTokenFromRequest(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"bearer", "Bearer abc123", "abc123"},
		{"lowercase scheme", "bearer abc123", "abc123"},
		{"basic is not bearer", "Basic abc123", ""},
		{"empty", "", ""},
		{"scheme only", "Bearer", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := BearerTokenFromRequest(r); got != tc.want {
				t.Fatalf("BearerTokenFromRequest() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := BearerTokenFromRequest(nil); got != "" {
		t.Fatalf("expected empty for a nil request, got %q", got)
	}
}

// compile-time assertion that the stub satisfies the domain interface
var _ users.Service = (*userServiceStub)(nil)
