package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/config"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/user"
)

func TestLoadSessionAttachesCurrentUserFromCookie(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	h := testHandler(t)
	u, rawToken, _ := insertUserAndSession(t, ctx, h.users)

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(&http.Cookie{Name: h.sessionCookieName, Value: rawToken})
	rec := httptest.NewRecorder()

	called := false
	h.loadSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		current := currentUserFromContext(r)
		if current == nil {
			t.Fatalf("expected current user in context")
		}
		if current.ID != u.ID {
			t.Fatalf("unexpected user id: got %d want %d", current.ID, u.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req.WithContext(ctx))

	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestLoadSessionExpiredOrRevokedClearsCookieAndSkipsAuthContext(t *testing.T) {
	t.Run("expired session", func(t *testing.T) {
		ctx, cleanup := withTx(t)
		defer cleanup()

		h := testHandler(t)
		_, rawToken, sessionID := insertUserAndSession(t, ctx, h.users)
		_, err := postgres.DBFromContext(ctx, integrationPool).Exec(ctx, `
			update user_sessions
			set expires_at = now() - interval '1 minute'
			where id = $1
		`, sessionID)
		if err != nil {
			t.Fatalf("expire session: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/account", nil)
		req.AddCookie(&http.Cookie{Name: h.sessionCookieName, Value: rawToken})
		rec := httptest.NewRecorder()

		h.loadSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if currentUserFromContext(r) != nil {
				t.Fatalf("did not expect current user for expired session")
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(rec, req.WithContext(ctx))

		assertCookieCleared(t, rec, h.sessionCookieName)
	})

	t.Run("revoked session", func(t *testing.T) {
		ctx, cleanup := withTx(t)
		defer cleanup()

		h := testHandler(t)
		_, rawToken, sessionID := insertUserAndSession(t, ctx, h.users)
		_, err := postgres.DBFromContext(ctx, integrationPool).Exec(ctx, `
			update user_sessions
			set revoked_at = now()
			where id = $1
		`, sessionID)
		if err != nil {
			t.Fatalf("revoke session: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/account", nil)
		req.AddCookie(&http.Cookie{Name: h.sessionCookieName, Value: rawToken})
		rec := httptest.NewRecorder()

		h.loadSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if currentUserFromContext(r) != nil {
				t.Fatalf("did not expect current user for revoked session")
			}
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(rec, req.WithContext(ctx))

		assertCookieCleared(t, rec, h.sessionCookieName)
	})
}

func TestRequireAuthAndRequireGuest(t *testing.T) {
	h := testHandler(t)

	t.Run("requireAuth redirects guest", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/account", nil)
		rec := httptest.NewRecorder()
		h.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call downstream")
		})).ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "/auth/login" {
			t.Fatalf("unexpected redirect: %q", got)
		}
	})

	t.Run("requireGuest redirects authenticated user", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		req = req.WithContext(context.WithValue(req.Context(), currentUserContextKey, &user.User{ID: 1}))
		rec := httptest.NewRecorder()
		h.requireGuest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call downstream")
		})).ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		if got := rec.Header().Get("Location"); got != "/account" {
			t.Fatalf("unexpected redirect: %q", got)
		}
	})
}

func TestLogoutDeletesCurrentSessionAndClearsCookie(t *testing.T) {
	ctx, cleanup := withTx(t)
	defer cleanup()

	h := testHandler(t)
	_, rawToken, _ := insertUserAndSession(t, ctx, h.users)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: h.sessionCookieName, Value: rawToken})
	rec := httptest.NewRecorder()

	h.logout(rec, req.WithContext(ctx))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/auth/login" {
		t.Fatalf("unexpected redirect: %q", got)
	}

	if _, _, err := h.users.FindSessionAndUserByTokenHash(ctx, hashToken(rawToken)); err == nil {
		t.Fatalf("expected session to be deleted")
	}

	foundClear := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == h.sessionCookieName && c.MaxAge < 0 {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Fatalf("expected cleared session cookie")
	}
}

func testHandler(t *testing.T) handler {
	t.Helper()
	cfg := config.Config{
		AppName: "Go Starter",
		AppEnv:  "test",
		AppURL:  "http://127.0.0.1:8080",
		Auth: config.AuthConfig{
			SessionCookieName: "test_session",
			SessionTTL:        30 * 24 * time.Hour,
		},
	}
	return newHandler(integrationPool, cfg)
}

func insertUserAndSession(t *testing.T, ctx context.Context, store *postgres.UserAuthStore) (user.User, string, int64) {
	t.Helper()
	suffix := time.Now().UnixNano()

	profile := user.SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-test-" + strconv.FormatInt(suffix, 10),
		Email:          "tester+" + strconv.FormatInt(suffix, 10) + "@example.com",
		EmailVerified:  true,
		Name:           "Tester",
		Username:       "tester",
	}
	u, err := store.CreateUserWithIdentity(ctx, profile)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rawToken := "raw-test-token-" + strconv.FormatInt(suffix, 10)
	err = store.CreateSession(ctx, user.Session{
		UserID:     u.ID,
		TokenHash:  hashToken(rawToken),
		ExpiresAt:  time.Now().Add(24 * time.Hour),
		LastSeenAt: time.Now(),
		IP:         "127.0.0.1",
		UserAgent:  "test",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	var sessionID int64
	err = postgres.DBFromContext(ctx, integrationPool).QueryRow(ctx, `
		select id
		from user_sessions
		where token_hash = $1
	`, hashToken(rawToken)).Scan(&sessionID)
	if err != nil {
		t.Fatalf("lookup session id: %v", err)
	}
	return u, rawToken, sessionID
}

func assertCookieCleared(t *testing.T, rec *httptest.ResponseRecorder, cookieName string) {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName && c.MaxAge < 0 {
			return
		}
	}
	t.Fatalf("expected cleared cookie %q", cookieName)
}
