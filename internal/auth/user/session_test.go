package user_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	usersession "github.com/benpsk/go-starter/internal/auth/user"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
	"github.com/benpsk/go-starter/internal/users"
)

const cookieName = "go_starter_session"

// newManager builds a manager bound to a rolled-back transaction, along with a
// seeded account to open sessions for.
func newManager(t *testing.T, secure bool) (*usersession.Manager, users.User, func()) {
	t.Helper()

	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-session-1",
		Email:          "session@example.com",
		Name:           "Session Owner",
		AvatarURL:      "https://img.example.com/s.png",
	})
	if err != nil {
		done()
		t.Fatalf("seed user: %v", err)
	}

	return usersession.NewManager(tx, cookieName, time.Hour, secure), owner, done
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == cookieName {
			return cookie
		}
	}
	t.Fatalf("no session cookie in the response")
	return nil
}

func TestIssueThenValidate(t *testing.T) {
	manager, owner, done := newManager(t, false)
	defer done()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(req.Context(), rec, req, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	cookie := sessionCookie(t, rec)
	if cookie.Value == "" || !cookie.HttpOnly || cookie.Path != "/" {
		t.Fatalf("unexpected cookie: %+v", cookie)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("OAuth redirects need SameSite=Lax, got %v", cookie.SameSite)
	}
	if cookie.Secure {
		t.Fatalf("expected an insecure cookie when secure is off")
	}

	next := httptest.NewRequest(http.MethodGet, "/account", nil)
	next.AddCookie(cookie)
	claims, err := manager.Validate(next)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if claims.ID != owner.ID || claims.Email != owner.Email {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if claims.DisplayName != owner.DisplayName || claims.AvatarURL != owner.AvatarURL {
		t.Fatalf("expected the profile to be carried in the claims: %+v", claims)
	}
}

func TestIssueSetsSecureCookieWhenConfigured(t *testing.T) {
	manager, owner, done := newManager(t, true)
	defer done()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(req.Context(), rec, req, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if !sessionCookie(t, rec).Secure {
		t.Fatalf("expected a Secure cookie")
	}
}

func TestIssueRejectsMissingPrincipal(t *testing.T) {
	manager, _, done := newManager(t, false)
	defer done()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(req.Context(), rec, req, 0); err == nil {
		t.Fatalf("expected an error for a zero user id")
	}
}

func TestValidateRejections(t *testing.T) {
	manager, owner, done := newManager(t, false)
	defer done()

	rec := httptest.NewRecorder()
	issue := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(issue.Context(), rec, issue, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	valid := sessionCookie(t, rec)

	cases := []struct {
		name    string
		cookie  *http.Cookie
		wantErr error
	}{
		{"no cookie", nil, usersession.ErrNoSession},
		{"blank cookie", &http.Cookie{Name: cookieName, Value: "   "}, usersession.ErrNoSession},
		{"unknown token", &http.Cookie{Name: cookieName, Value: "not-a-session"}, usersession.ErrInvalidSession},
		{"another cookie name", &http.Cookie{Name: "other", Value: valid.Value}, usersession.ErrNoSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			if _, err := manager.Validate(req); err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}

	if _, err := manager.Validate(nil); err != usersession.ErrNoSession {
		t.Fatalf("expected ErrNoSession for a nil request, got %v", err)
	}
}

func TestClearRemovesTheSession(t *testing.T) {
	manager, owner, done := newManager(t, false)
	defer done()

	issued := httptest.NewRecorder()
	issueReq := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(issueReq.Context(), issued, issueReq, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	cookie := sessionCookie(t, issued)

	cleared := httptest.NewRecorder()
	clearReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	clearReq.AddCookie(cookie)
	manager.Clear(clearReq.Context(), cleared, clearReq)

	if got := sessionCookie(t, cleared); got.MaxAge != -1 {
		t.Fatalf("expected the cookie to be expired, got %+v", got)
	}

	// The row is gone, so the previously valid token no longer resolves.
	after := httptest.NewRequest(http.MethodGet, "/account", nil)
	after.AddCookie(cookie)
	if _, err := manager.Validate(after); err != usersession.ErrInvalidSession {
		t.Fatalf("expected the session to be gone, got %v", err)
	}
}

func TestClearWithoutASessionStillExpiresTheCookie(t *testing.T) {
	manager, _, done := newManager(t, false)
	defer done()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	manager.Clear(req.Context(), rec, req)

	if got := sessionCookie(t, rec); got.MaxAge != -1 {
		t.Fatalf("expected an expiring cookie, got %+v", got)
	}
}

func TestNewManagerAppliesDefaults(t *testing.T) {
	manager := usersession.NewManager(nil, "   ", 0, false)

	if manager.CookieName() != "session" {
		t.Fatalf("expected a fallback cookie name, got %q", manager.CookieName())
	}
}

func TestValidateRejectsAnExpiredSession(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-session-expired",
		Email:          "expired-session@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// A negative lifetime writes a session that is already past its expiry.
	manager := usersession.NewManager(tx, cookieName, time.Hour, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(req.Context(), rec, req, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	cookie := sessionCookie(t, rec)

	if _, err := tx.Exec(ctx, `update user_sessions set expires_at = now() - interval '1 hour'`); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	after := httptest.NewRequest(http.MethodGet, "/account", nil)
	after.AddCookie(cookie)
	if _, err := manager.Validate(after); err != usersession.ErrInvalidSession {
		t.Fatalf("expected an expired session to be rejected, got %v", err)
	}
}

func TestValidateRejectsARevokedSession(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-session-revoked",
		Email:          "revoked-session@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	manager := usersession.NewManager(tx, cookieName, time.Hour, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := manager.Issue(req.Context(), rec, req, owner.ID); err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	cookie := sessionCookie(t, rec)

	if _, err := tx.Exec(ctx, `update user_sessions set revoked_at = now()`); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	after := httptest.NewRequest(http.MethodGet, "/account", nil)
	after.AddCookie(cookie)
	if _, err := manager.Validate(after); err != usersession.ErrInvalidSession {
		t.Fatalf("expected a revoked session to be rejected, got %v", err)
	}
}

func TestPruneExpired(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-session-prune",
		Email:          "prune-session@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	manager := usersession.NewManager(tx, cookieName, time.Hour, false)
	// Three live sessions, then age two of them past the grace period.
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if err := manager.Issue(req.Context(), rec, req, owner.ID); err != nil {
			t.Fatalf("Issue failed: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		update user_sessions
		set expires_at = now() - interval '48 hours'
		where id in (select id from user_sessions order by id limit 1)
	`); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		update user_sessions
		set revoked_at = now() - interval '48 hours'
		where id in (select id from user_sessions order by id offset 1 limit 1)
	`); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	removed, err := manager.PruneExpired(ctx, 24*time.Hour, time.Now())
	if err != nil {
		t.Fatalf("PruneExpired failed: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 sessions removed, got %d", removed)
	}

	var remaining int
	if err := tx.QueryRow(ctx, `select count(*) from user_sessions`).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected the live session to survive, %d remain", remaining)
	}
}

func TestPruneExpiredRejectsANegativeGrace(t *testing.T) {
	pool := testutil.SetupDB(t)
	_, tx, done := testutil.BeginTx(t, pool)
	defer done()

	manager := usersession.NewManager(tx, cookieName, time.Hour, false)
	if _, err := manager.PruneExpired(context.Background(), -time.Hour, time.Now()); err == nil {
		t.Fatalf("expected a negative grace period to be rejected")
	}
}

func TestPruneExpiredOnAnEmptyTable(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, tx, done := testutil.BeginTx(t, pool)
	defer done()

	manager := usersession.NewManager(tx, cookieName, time.Hour, false)
	removed, err := manager.PruneExpired(ctx, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("pruning nothing must succeed: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed, got %d", removed)
	}
}
