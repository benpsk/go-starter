package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExchangeAndVerifyRejectsUnknownProvider(t *testing.T) {
	verifier := NewSocialVerifier()

	_, err := verifier.ExchangeAndVerify(context.Background(), "apple", "code", "verifier", "app://cb", OAuthProviderConfig{
		ClientID:     "id",
		ClientSecret: "secret",
	})
	if !errors.Is(err, errVerifyInvalidInput) {
		t.Fatalf("expected an invalid input error, got %v", err)
	}
}

func TestExchangeAndVerifyRequiresCredentialsBeforeAnyNetworkCall(t *testing.T) {
	verifier := NewSocialVerifier()

	cases := []struct {
		name     string
		provider string
		code     string
		cfg      OAuthProviderConfig
	}{
		{"google without a code", ProviderGoogle, "", OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"}},
		{"google without credentials", ProviderGoogle, "code", OAuthProviderConfig{}},
		{"google with half credentials", ProviderGoogle, "code", OAuthProviderConfig{ClientID: "id"}},
		{"github without a code", ProviderGitHub, "", OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"}},
		{"github without credentials", ProviderGitHub, "code", OAuthProviderConfig{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verifier.ExchangeAndVerify(context.Background(), tc.provider, tc.code, "verifier", "app://cb", tc.cfg)
			if !errors.Is(err, errVerifyInvalidInput) {
				t.Fatalf("expected an invalid input error, got %v", err)
			}
		})
	}
}

func TestParseTruthy(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"string true", "true", true},
		{"string TRUE", "TRUE", true},
		{"string one", "1", true},
		{"string false", "false", false},
		{"string empty", "", false},
		{"number", 1, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseTruthy(tc.in); got != tc.want {
				t.Fatalf("parseTruthy(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestProviderConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		cfg  OAuthProviderConfig
		want bool
	}{
		{"complete", OAuthProviderConfig{ClientID: "id", ClientSecret: "secret"}, true},
		{"missing secret", OAuthProviderConfig{ClientID: "id"}, false},
		{"missing id", OAuthProviderConfig{ClientSecret: "secret"}, false},
		{"blank", OAuthProviderConfig{ClientID: "  ", ClientSecret: "  "}, false},
		{"empty", OAuthProviderConfig{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

// providerServer stands in for a provider's HTTP API so the exchange paths can
// be exercised without reaching the internet.
type providerServer struct {
	server  *httptest.Server
	handler map[string]http.HandlerFunc
}

func newProviderServer(t *testing.T) *providerServer {
	t.Helper()
	ps := &providerServer{handler: map[string]http.HandlerFunc{}}
	ps.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := ps.handler[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(ps.server.Close)
	return ps
}

func (p *providerServer) on(path string, status int, body string) {
	p.handler[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func (p *providerServer) verifier() *socialVerifier {
	base := p.server.URL
	return &socialVerifier{
		httpClient: p.server.Client(),
		endpoints: providerEndpoints{
			googleToken:     base + "/google/token",
			googleTokenInfo: base + "/google/tokeninfo",
			githubToken:     base + "/github/token",
			githubUser:      base + "/github/user",
			githubEmails:    base + "/github/emails",
		},
	}
}

func googleCfg() OAuthProviderConfig {
	return OAuthProviderConfig{ClientID: "google-id", ClientSecret: "google-secret"}
}

func githubCfg() OAuthProviderConfig {
	return OAuthProviderConfig{ClientID: "github-id", ClientSecret: "github-secret"}
}

func TestGoogleExchangeReturnsAVerifiedProfile(t *testing.T) {
	ps := newProviderServer(t)
	ps.on("/google/token", http.StatusOK, `{"id_token":"id-token"}`)
	ps.on("/google/tokeninfo", http.StatusOK, `{
		"sub":"g-1","email":"Alice@Example.com","email_verified":"true",
		"name":"Alice","picture":"https://img/a.png",
		"aud":"google-id","iss":"https://accounts.google.com"
	}`)

	profile, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGoogle, "code", "verifier", "app://cb", googleCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Provider != ProviderGoogle || profile.ProviderUserID != "g-1" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.Email != "alice@example.com" {
		t.Fatalf("expected the email to be lowercased, got %q", profile.Email)
	}
	if !profile.EmailVerified || profile.Name != "Alice" || profile.AvatarURL != "https://img/a.png" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestGoogleExchangeRejectsUntrustworthyTokens(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		tokenInfo string
		infoCode  int
	}{
		{
			name:      "audience belongs to another client",
			token:     `{"id_token":"t"}`,
			tokenInfo: `{"sub":"g-1","aud":"someone-else","iss":"https://accounts.google.com"}`,
			infoCode:  http.StatusOK,
		},
		{
			name:      "issuer is not google",
			token:     `{"id_token":"t"}`,
			tokenInfo: `{"sub":"g-1","aud":"google-id","iss":"https://evil.example.com"}`,
			infoCode:  http.StatusOK,
		},
		{
			name:      "no subject",
			token:     `{"id_token":"t"}`,
			tokenInfo: `{"aud":"google-id","iss":"https://accounts.google.com"}`,
			infoCode:  http.StatusOK,
		},
		{
			name:      "tokeninfo rejects the token",
			token:     `{"id_token":"t"}`,
			tokenInfo: `{}`,
			infoCode:  http.StatusUnauthorized,
		},
		{
			name:      "token response carries nothing usable",
			token:     `{}`,
			tokenInfo: `{}`,
			infoCode:  http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := newProviderServer(t)
			ps.on("/google/token", http.StatusOK, tc.token)
			ps.on("/google/tokeninfo", tc.infoCode, tc.tokenInfo)

			if _, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGoogle, "code", "verifier", "app://cb", googleCfg()); err == nil {
				t.Fatalf("expected the exchange to be refused")
			}
		})
	}
}

func TestGoogleExchangeFallsBackToTheAccessToken(t *testing.T) {
	ps := newProviderServer(t)
	ps.on("/google/token", http.StatusOK, `{"access_token":"at"}`)
	ps.on("/google/tokeninfo", http.StatusOK, `{
		"sub":"g-2","email":"b@example.com","email_verified":true,
		"aud":"google-id","iss":"accounts.google.com"
	}`)

	profile, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGoogle, "code", "verifier", "app://cb", googleCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.ProviderUserID != "g-2" || !profile.EmailVerified {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestGoogleExchangeFailsWhenTheTokenEndpointErrors(t *testing.T) {
	ps := newProviderServer(t)
	ps.on("/google/token", http.StatusBadRequest, `{"error":"invalid_grant"}`)

	if _, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGoogle, "code", "verifier", "app://cb", googleCfg()); err == nil {
		t.Fatalf("expected a failed token exchange to be refused")
	}
}

func TestGitHubExchangeReturnsAVerifiedProfile(t *testing.T) {
	ps := newProviderServer(t)
	ps.on("/github/token", http.StatusOK, `{"access_token":"gho_x"}`)
	ps.on("/github/user", http.StatusOK, `{
		"id":42,"login":"octocat","name":"The Octocat",
		"email":"public@example.com","avatar_url":"https://img/o.png"
	}`)
	ps.on("/github/emails", http.StatusOK, `[
		{"email":"secondary@example.com","primary":false,"verified":true},
		{"email":"Primary@Example.com","primary":true,"verified":true}
	]`)

	profile, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGitHub, "code", "verifier", "app://cb", githubCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.Provider != ProviderGitHub || profile.ProviderUserID != "42" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if profile.Email != "primary@example.com" || !profile.EmailVerified {
		t.Fatalf("expected the verified primary address, got %+v", profile)
	}
	if profile.Name != "The Octocat" || profile.Username != "octocat" {
		t.Fatalf("unexpected profile: %+v", profile)
	}
}

func TestGitHubExchangeEmailSelection(t *testing.T) {
	cases := []struct {
		name         string
		user         string
		emails       string
		emailsCode   int
		wantEmail    string
		wantVerified bool
	}{
		{
			name:         "falls back to any verified address",
			user:         `{"id":1,"login":"a"}`,
			emails:       `[{"email":"only@example.com","primary":false,"verified":true}]`,
			emailsCode:   http.StatusOK,
			wantEmail:    "only@example.com",
			wantVerified: true,
		},
		{
			name:         "keeps the profile address when none are verified",
			user:         `{"id":1,"login":"a","email":"profile@example.com"}`,
			emails:       `[{"email":"unverified@example.com","primary":true,"verified":false}]`,
			emailsCode:   http.StatusOK,
			wantEmail:    "profile@example.com",
			wantVerified: true,
		},
		{
			name:         "tolerates an unavailable emails endpoint",
			user:         `{"id":1,"login":"a","email":"profile@example.com"}`,
			emails:       `{}`,
			emailsCode:   http.StatusForbidden,
			wantEmail:    "profile@example.com",
			wantVerified: true,
		},
		{
			name:         "no address at all",
			user:         `{"id":1,"login":"a"}`,
			emails:       `[]`,
			emailsCode:   http.StatusOK,
			wantEmail:    "",
			wantVerified: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := newProviderServer(t)
			ps.on("/github/token", http.StatusOK, `{"access_token":"gho_x"}`)
			ps.on("/github/user", http.StatusOK, tc.user)
			ps.on("/github/emails", tc.emailsCode, tc.emails)

			profile, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGitHub, "code", "verifier", "app://cb", githubCfg())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if profile.Email != tc.wantEmail || profile.EmailVerified != tc.wantVerified {
				t.Fatalf("email = %q verified = %v, want %q / %v", profile.Email, profile.EmailVerified, tc.wantEmail, tc.wantVerified)
			}
			if profile.Name != "a" {
				t.Fatalf("expected the login to become the name, got %q", profile.Name)
			}
		})
	}
}

func TestGitHubExchangeFailureModes(t *testing.T) {
	cases := []struct {
		name      string
		token     string
		tokenCode int
		user      string
		userCode  int
	}{
		{"token endpoint errors", `{"error":"bad_verification_code"}`, http.StatusUnauthorized, `{}`, http.StatusOK},
		{"no access token", `{}`, http.StatusOK, `{}`, http.StatusOK},
		{"user endpoint errors", `{"access_token":"gho_x"}`, http.StatusOK, `{}`, http.StatusUnauthorized},
		{"user has no id", `{"access_token":"gho_x"}`, http.StatusOK, `{"login":"a"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := newProviderServer(t)
			ps.on("/github/token", tc.tokenCode, tc.token)
			ps.on("/github/user", tc.userCode, tc.user)
			ps.on("/github/emails", http.StatusOK, `[]`)

			if _, err := ps.verifier().ExchangeAndVerify(context.Background(), ProviderGitHub, "code", "verifier", "app://cb", githubCfg()); err == nil {
				t.Fatalf("expected the exchange to be refused")
			}
		})
	}
}

func TestExchangeFailsWhenTheProviderIsUnreachable(t *testing.T) {
	verifier := &socialVerifier{
		httpClient: &http.Client{Timeout: time.Second},
		endpoints: providerEndpoints{
			googleToken: "http://127.0.0.1:1/token",
			githubToken: "http://127.0.0.1:1/token",
		},
	}

	if _, err := verifier.ExchangeAndVerify(context.Background(), ProviderGoogle, "code", "v", "app://cb", googleCfg()); err == nil {
		t.Fatalf("expected an unreachable google endpoint to fail")
	}
	if _, err := verifier.ExchangeAndVerify(context.Background(), ProviderGitHub, "code", "v", "app://cb", githubCfg()); err == nil {
		t.Fatalf("expected an unreachable github endpoint to fail")
	}
}
