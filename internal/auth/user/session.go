// Package user issues and validates the database-backed session cookies used by
// the browser transport. It talks to the session tables directly: a session is
// an authentication mechanism rather than a domain concept, so it does not get
// a service and repository pair of its own.
package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNoSession indicates the request carries no session cookie.
var ErrNoSession = errors.New("user session: no session")

// ErrInvalidSession indicates the session is unknown, expired, or revoked.
var ErrInvalidSession = errors.New("user session: invalid")

// touchInterval throttles last_seen_at writes so an active session does not
// issue an update on every request.
const touchInterval = 10 * time.Minute

// Querier is implemented by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Claims is the authenticated principal a valid session resolves to.
type Claims struct {
	ID          int64
	Email       string
	DisplayName string
	AvatarURL   string
}

// Manager issues and validates opaque session cookies.
type Manager struct {
	db         Querier
	cookieName string
	lifetime   time.Duration
	secure     bool
}

// NewManager constructs a session manager.
func NewManager(db Querier, cookieName string, ttl time.Duration, secure bool) *Manager {
	if strings.TrimSpace(cookieName) == "" {
		cookieName = "session"
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Manager{db: db, cookieName: cookieName, lifetime: ttl, secure: secure}
}

// CookieName reports the cookie this manager reads and writes.
func (m *Manager) CookieName() string {
	return m.cookieName
}

// Issue creates a session row and sets the opaque cookie on the response.
func (m *Manager) Issue(ctx context.Context, w http.ResponseWriter, r *http.Request, userID int64) error {
	if userID <= 0 {
		return fmt.Errorf("issue user session: missing principal")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rawToken, err := randomToken(32)
	if err != nil {
		return fmt.Errorf("issue user session: token generation failed: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(m.lifetime)
	ip, ua := requestMeta(r)

	_, err = m.db.Exec(ctx, `
		insert into user_sessions (user_id, token_hash, expires_at, last_seen_at, ip, user_agent)
		values ($1, $2, $3, $4, nullif($5, ''), nullif($6, ''))
	`, userID, hashToken(rawToken), expiresAt, now, ip, ua)
	if err != nil {
		return fmt.Errorf("issue user session: insert session: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		// Lax is required for OAuth redirect flows (provider -> callback -> app).
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(m.lifetime.Seconds()),
	})
	return nil
}

// Clear deletes the backing session row when present and expires the cookie.
func (m *Manager) Clear(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if ctx == nil {
		ctx = context.Background()
	}
	if token := m.tokenFromRequest(r); token != "" {
		_, _ = m.db.Exec(ctx, `delete from user_sessions where token_hash = $1`, hashToken(token))
	}

	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Validate resolves the request's session cookie into its principal.
func (m *Manager) Validate(r *http.Request) (Claims, error) {
	token := m.tokenFromRequest(r)
	if token == "" {
		return Claims{}, ErrNoSession
	}

	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}

	var (
		claims     Claims
		sessionID  int64
		expiresAt  time.Time
		lastSeenAt time.Time
		revokedAt  *time.Time
	)
	err := m.db.QueryRow(ctx, `
		select
			s.id,
			s.expires_at,
			s.last_seen_at,
			s.revoked_at,
			u.id,
			coalesce(u.email, ''),
			u.display_name,
			coalesce(u.avatar_url, '')
		from user_sessions s
		join users u on u.id = s.user_id
		where s.token_hash = $1
	`, hashToken(token)).Scan(
		&sessionID,
		&expiresAt,
		&lastSeenAt,
		&revokedAt,
		&claims.ID,
		&claims.Email,
		&claims.DisplayName,
		&claims.AvatarURL,
	)
	if err != nil {
		return Claims{}, ErrInvalidSession
	}

	now := time.Now()
	if revokedAt != nil || now.After(expiresAt) {
		return Claims{}, ErrInvalidSession
	}

	if now.Sub(lastSeenAt) >= touchInterval {
		_, _ = m.db.Exec(ctx, `update user_sessions set last_seen_at = $2 where id = $1`, sessionID, now)
	}

	return claims, nil
}

func (m *Manager) tokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(m.cookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func randomToken(n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func requestMeta(r *http.Request) (ip string, userAgent string) {
	if r == nil {
		return "", ""
	}
	ip = strings.TrimSpace(r.RemoteAddr)
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = strings.Trim(ip[:idx], "[]")
	}
	return ip, strings.TrimSpace(r.UserAgent())
}

// PruneExpired deletes sessions that expired, or were revoked, more than
// olderThan ago, and reports how many were removed. Nothing else removes these
// rows, so without a periodic prune the table grows without bound.
func (m *Manager) PruneExpired(ctx context.Context, olderThan time.Duration, now time.Time) (int64, error) {
	if olderThan < 0 {
		return 0, fmt.Errorf("prune user sessions: grace period must not be negative")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cutoff := now.Add(-olderThan)
	tag, err := m.db.Exec(ctx, `
		delete from user_sessions
		where expires_at < $1
		   or (revoked_at is not null and revoked_at < $1)
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune user sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
