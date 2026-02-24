package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserAuthStore struct {
	db *pgxpool.Pool
}

func NewUserAuthStore(pool *pgxpool.Pool) *UserAuthStore {
	return &UserAuthStore{db: pool}
}

func (s *UserAuthStore) FindByIdentity(ctx context.Context, provider, providerUserID string) (user.User, error) {
	db := DBFromContext(ctx, s.db)
	var out user.User
	var email sql.NullString
	err := db.QueryRow(ctx, `
		select u.id, coalesce(u.email, ''), u.display_name, coalesce(u.avatar_url, ''), u.created_at, u.updated_at
		from user_identities ui
		join users u on u.id = ui.user_id
		where ui.provider = $1 and ui.provider_user_id = $2
	`, strings.TrimSpace(strings.ToLower(provider)), strings.TrimSpace(providerUserID)).Scan(
		&out.ID, &email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, user.ErrNotFound
		}
		return user.User{}, fmt.Errorf("find user by identity: %w", err)
	}
	out.Email = email.String
	return out, nil
}

func (s *UserAuthStore) FindByEmail(ctx context.Context, email string) (user.User, error) {
	db := DBFromContext(ctx, s.db)
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return user.User{}, user.ErrNotFound
	}
	var out user.User
	err := db.QueryRow(ctx, `
		select id, coalesce(email, ''), display_name, coalesce(avatar_url, ''), created_at, updated_at
		from users
		where email = $1
	`, email).Scan(&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, user.ErrNotFound
		}
		return user.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return out, nil
}

func (s *UserAuthStore) FindByID(ctx context.Context, id int64) (user.User, error) {
	db := DBFromContext(ctx, s.db)
	var out user.User
	err := db.QueryRow(ctx, `
		select id, coalesce(email, ''), display_name, coalesce(avatar_url, ''), created_at, updated_at
		from users
		where id = $1
	`, id).Scan(&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, user.ErrNotFound
		}
		return user.User{}, fmt.Errorf("find user by id: %w", err)
	}
	return out, nil
}

func (s *UserAuthStore) CreateUserWithIdentity(ctx context.Context, profile user.SocialProfile) (user.User, error) {
	if err := profile.Validate(); err != nil {
		return user.User{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return user.User{}, fmt.Errorf("begin create user with identity: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var existingID int64
	email := strings.TrimSpace(strings.ToLower(profile.Email))
	if email != "" {
		err = tx.QueryRow(ctx, `select id from users where email = $1`, email).Scan(&existingID)
		if err == nil && existingID > 0 {
			return user.User{}, user.ErrEmailConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return user.User{}, fmt.Errorf("check email conflict: %w", err)
		}
	}

	displayName := strings.TrimSpace(profile.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(profile.Username)
	}
	if displayName == "" {
		displayName = "User"
	}

	var out user.User
	var nullableEmail any
	if email != "" {
		nullableEmail = email
	}
	err = tx.QueryRow(ctx, `
		insert into users (email, display_name, avatar_url)
		values ($1, $2, nullif($3, ''))
		returning id, coalesce(email, ''), display_name, coalesce(avatar_url, ''), created_at, updated_at
	`, nullableEmail, displayName, strings.TrimSpace(profile.AvatarURL)).Scan(
		&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return user.User{}, user.ErrEmailConflict
		}
		return user.User{}, fmt.Errorf("insert user: %w", err)
	}

	_, err = tx.Exec(ctx, `
		insert into user_identities (
			user_id, provider, provider_user_id, provider_email, provider_name, provider_handle, avatar_url
		) values ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, ''))
	`, out.ID,
		strings.TrimSpace(strings.ToLower(profile.Provider)),
		strings.TrimSpace(profile.ProviderUserID),
		email,
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.Username),
		strings.TrimSpace(profile.AvatarURL),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return user.User{}, user.ErrIdentityConflict
		}
		return user.User{}, fmt.Errorf("insert identity: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return user.User{}, fmt.Errorf("commit create user with identity: %w", err)
	}
	return out, nil
}

func (s *UserAuthStore) UpdateUserFromProfile(ctx context.Context, userID int64, profile user.SocialProfile) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `
		update users
		set display_name = case when nullif($2, '') is not null then $2 else display_name end,
		    avatar_url = case when nullif($3, '') is not null then $3 else avatar_url end
		where id = $1
	`, userID, strings.TrimSpace(profile.Name), strings.TrimSpace(profile.AvatarURL))
	if err != nil {
		return fmt.Errorf("update user from profile: %w", err)
	}
	_, err = db.Exec(ctx, `
		update user_identities
		set provider_email = nullif($3, ''),
		    provider_name = nullif($4, ''),
		    provider_handle = nullif($5, ''),
		    avatar_url = nullif($6, '')
		where user_id = $1 and provider = $2
	`, userID,
		strings.TrimSpace(strings.ToLower(profile.Provider)),
		strings.TrimSpace(strings.ToLower(profile.Email)),
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.Username),
		strings.TrimSpace(profile.AvatarURL),
	)
	if err != nil {
		return fmt.Errorf("update identity from profile: %w", err)
	}
	return nil
}

func (s *UserAuthStore) ListIdentitiesByUserID(ctx context.Context, userID int64) ([]user.Identity, error) {
	db := DBFromContext(ctx, s.db)
	rows, err := db.Query(ctx, `
		select id, user_id, provider, provider_user_id, coalesce(provider_email, ''), coalesce(provider_name, ''), coalesce(provider_handle, ''), coalesce(avatar_url, ''), created_at, updated_at
		from user_identities
		where user_id = $1
		order by provider asc
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	out := make([]user.Identity, 0)
	for rows.Next() {
		var item user.Identity
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Provider, &item.ProviderUserID, &item.ProviderEmail,
			&item.ProviderName, &item.ProviderHandle, &item.AvatarURL, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identities: %w", err)
	}
	return out, nil
}

func (s *UserAuthStore) CreateSession(ctx context.Context, sess user.Session) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `
		insert into user_sessions (user_id, token_hash, expires_at, last_seen_at, ip, user_agent)
		values ($1, $2, $3, coalesce($4, now()), nullif($5, ''), nullif($6, ''))
	`, sess.UserID, sess.TokenHash, sess.ExpiresAt, sess.LastSeenAt, strings.TrimSpace(sess.IP), strings.TrimSpace(sess.UserAgent))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *UserAuthStore) FindSessionAndUserByTokenHash(ctx context.Context, tokenHash string) (user.Session, user.User, error) {
	db := DBFromContext(ctx, s.db)
	var sess user.Session
	var u user.User
	err := db.QueryRow(ctx, `
		select
			s.id, s.user_id, s.token_hash, s.expires_at, s.created_at, s.last_seen_at,
			coalesce(s.ip, ''), coalesce(s.user_agent, ''), s.revoked_at,
			u.id, coalesce(u.email, ''), u.display_name, coalesce(u.avatar_url, ''), u.created_at, u.updated_at
		from user_sessions s
		join users u on u.id = s.user_id
		where s.token_hash = $1
	`, strings.TrimSpace(tokenHash)).Scan(
		&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt, &sess.LastSeenAt, &sess.IP, &sess.UserAgent, &sess.RevokedAt,
		&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.Session{}, user.User{}, user.ErrNotFound
		}
		return user.Session{}, user.User{}, fmt.Errorf("find session by token hash: %w", err)
	}
	return sess, u, nil
}

func (s *UserAuthStore) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `delete from user_sessions where token_hash = $1`, strings.TrimSpace(tokenHash))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *UserAuthStore) TouchSession(ctx context.Context, sessionID int64, at time.Time) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `update user_sessions set last_seen_at = $2 where id = $1`, sessionID, at)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (s *UserAuthStore) CreateAPIRefreshToken(ctx context.Context, token user.APIRefreshToken) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `
		insert into api_refresh_tokens (user_id, family_id, token_hash, expires_at)
		values ($1, $2, $3, $4)
	`, token.UserID, strings.TrimSpace(token.FamilyID), strings.TrimSpace(token.TokenHash), token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create api refresh token: %w", err)
	}
	return nil
}

func (s *UserAuthStore) GetAPIRefreshTokenByHash(ctx context.Context, tokenHash string) (user.APIRefreshToken, error) {
	db := DBFromContext(ctx, s.db)
	var out user.APIRefreshToken
	err := db.QueryRow(ctx, `
		select id, user_id, family_id, token_hash, expires_at, created_at, last_used_at, revoked_at, replaced_by_token_id
		from api_refresh_tokens
		where token_hash = $1
	`, strings.TrimSpace(tokenHash)).Scan(
		&out.ID, &out.UserID, &out.FamilyID, &out.TokenHash, &out.ExpiresAt, &out.CreatedAt, &out.LastUsedAt, &out.RevokedAt, &out.ReplacedByTokenID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return user.APIRefreshToken{}, user.ErrNotFound
		}
		return user.APIRefreshToken{}, fmt.Errorf("get api refresh token: %w", err)
	}
	return out, nil
}

type APIRotateRefreshTokenResult struct {
	UserID        int64
	FamilyID      string
	ReuseDetected bool
	Authorized    bool
}

func (s *UserAuthStore) RotateAPIRefreshToken(ctx context.Context, oldTokenHash string, newToken user.APIRefreshToken, now time.Time) (APIRotateRefreshTokenResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return APIRotateRefreshTokenResult{}, fmt.Errorf("begin rotate api refresh token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var current user.APIRefreshToken
	err = tx.QueryRow(ctx, `
		select id, user_id, family_id, token_hash, expires_at, created_at, last_used_at, revoked_at, replaced_by_token_id
		from api_refresh_tokens
		where token_hash = $1
		for update
	`, strings.TrimSpace(oldTokenHash)).Scan(
		&current.ID, &current.UserID, &current.FamilyID, &current.TokenHash, &current.ExpiresAt, &current.CreatedAt, &current.LastUsedAt, &current.RevokedAt, &current.ReplacedByTokenID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIRotateRefreshTokenResult{Authorized: false}, nil
		}
		return APIRotateRefreshTokenResult{}, fmt.Errorf("select current api refresh token: %w", err)
	}

	if current.RevokedAt != nil || current.ReplacedByTokenID != nil || now.After(current.ExpiresAt) {
		_, _ = tx.Exec(ctx, `update api_refresh_tokens set revoked_at = coalesce(revoked_at, $2) where family_id = $1`, current.FamilyID, now)
		if err := tx.Commit(ctx); err != nil {
			return APIRotateRefreshTokenResult{}, fmt.Errorf("commit revoke family on reuse: %w", err)
		}
		return APIRotateRefreshTokenResult{
			UserID:        current.UserID,
			FamilyID:      current.FamilyID,
			ReuseDetected: true,
			Authorized:    false,
		}, nil
	}

	var newID int64
	userID := newToken.UserID
	if userID == 0 {
		userID = current.UserID
	}
	familyID := strings.TrimSpace(newToken.FamilyID)
	if familyID == "" {
		familyID = current.FamilyID
	}

	err = tx.QueryRow(ctx, `
		insert into api_refresh_tokens (user_id, family_id, token_hash, expires_at)
		values ($1, $2, $3, $4)
		returning id
	`, userID, familyID, strings.TrimSpace(newToken.TokenHash), newToken.ExpiresAt).Scan(&newID)
	if err != nil {
		return APIRotateRefreshTokenResult{}, fmt.Errorf("insert rotated api refresh token: %w", err)
	}

	_, err = tx.Exec(ctx, `
		update api_refresh_tokens
		set last_used_at = $2, revoked_at = $2, replaced_by_token_id = $3
		where id = $1
	`, current.ID, now, newID)
	if err != nil {
		return APIRotateRefreshTokenResult{}, fmt.Errorf("mark current api refresh token rotated: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return APIRotateRefreshTokenResult{}, fmt.Errorf("commit rotate api refresh token: %w", err)
	}

	return APIRotateRefreshTokenResult{
		UserID:     current.UserID,
		FamilyID:   current.FamilyID,
		Authorized: true,
	}, nil
}

func (s *UserAuthStore) RevokeAPIRefreshTokenByHash(ctx context.Context, tokenHash string, now time.Time) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `update api_refresh_tokens set revoked_at = coalesce(revoked_at, $2) where token_hash = $1`, strings.TrimSpace(tokenHash), now)
	if err != nil {
		return fmt.Errorf("revoke api refresh token: %w", err)
	}
	return nil
}

func (s *UserAuthStore) RevokeAPIRefreshTokenFamily(ctx context.Context, familyID string, now time.Time) error {
	db := DBFromContext(ctx, s.db)
	_, err := db.Exec(ctx, `update api_refresh_tokens set revoked_at = coalesce(revoked_at, $2) where family_id = $1`, strings.TrimSpace(familyID), now)
	if err != nil {
		return fmt.Errorf("revoke api refresh token family: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
