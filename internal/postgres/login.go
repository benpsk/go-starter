package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/jackc/pgx/v5"
)

// LoginRepository provides Postgres-backed storage for API refresh tokens.
type LoginRepository struct {
	db Querier
}

// NewLoginRepository constructs a login repository instance.
func NewLoginRepository(db Querier) *LoginRepository {
	return &LoginRepository{db: db}
}

// ErrNoRows reports a missing row for lookups that have no domain sentinel.
var ErrNoRows = errors.New("postgres: no rows")

const refreshTokenColumns = `id, user_id, family_id, token_hash, expires_at, created_at, last_used_at, revoked_at, replaced_by_token_id`

// CreateRefreshToken stores a new refresh token hash.
func (r *LoginRepository) CreateRefreshToken(ctx context.Context, token loginsvc.RefreshToken) error {
	_, err := QuerierFromContext(ctx, r.db).Exec(ctx, `
        insert into api_refresh_tokens (user_id, family_id, token_hash, expires_at)
        values ($1, $2, $3, $4)
    `, token.UserID, strings.TrimSpace(token.FamilyID), strings.TrimSpace(token.TokenHash), token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create api refresh token: %w", err)
	}
	return nil
}

// RotateRefreshToken swaps one refresh token for its successor inside a single
// transaction. A token that was already rotated, revoked, or expired means the
// credential leaked, so the caller is told to revoke the whole family.
func (r *LoginRepository) RotateRefreshToken(ctx context.Context, currentHash string, next loginsvc.RefreshToken, now time.Time) (loginsvc.RotationResult, error) {
	tx, err := QuerierFromContext(ctx, r.db).Begin(ctx)
	if err != nil {
		return loginsvc.RotationResult{}, fmt.Errorf("begin rotate api refresh token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	var current loginsvc.RefreshToken
	err = tx.QueryRow(ctx, `
        select `+refreshTokenColumns+`
        from api_refresh_tokens
        where token_hash = $1
        for update
    `, strings.TrimSpace(currentHash)).Scan(
		&current.ID, &current.UserID, &current.FamilyID, &current.TokenHash, &current.ExpiresAt,
		&current.CreatedAt, &current.LastUsedAt, &current.RevokedAt, &current.ReplacedByTokenID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return loginsvc.RotationResult{Authorized: false}, nil
		}
		return loginsvc.RotationResult{}, fmt.Errorf("select current api refresh token: %w", err)
	}

	if current.RevokedAt != nil || current.ReplacedByTokenID != nil || now.After(current.ExpiresAt) {
		if _, err := tx.Exec(ctx, `
            update api_refresh_tokens
            set revoked_at = coalesce(revoked_at, $2)
            where family_id = $1
        `, current.FamilyID, now); err != nil {
			return loginsvc.RotationResult{}, fmt.Errorf("revoke family on reuse: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return loginsvc.RotationResult{}, fmt.Errorf("commit revoke family on reuse: %w", err)
		}
		return loginsvc.RotationResult{
			UserID:        current.UserID,
			FamilyID:      current.FamilyID,
			ReuseDetected: true,
			Authorized:    false,
		}, nil
	}

	userID := next.UserID
	if userID == 0 {
		userID = current.UserID
	}
	familyID := strings.TrimSpace(next.FamilyID)
	if familyID == "" {
		familyID = current.FamilyID
	}

	var nextID int64
	err = tx.QueryRow(ctx, `
        insert into api_refresh_tokens (user_id, family_id, token_hash, expires_at)
        values ($1, $2, $3, $4)
        returning id
    `, userID, familyID, strings.TrimSpace(next.TokenHash), next.ExpiresAt).Scan(&nextID)
	if err != nil {
		return loginsvc.RotationResult{}, fmt.Errorf("insert rotated api refresh token: %w", err)
	}

	_, err = tx.Exec(ctx, `
        update api_refresh_tokens
        set last_used_at = $2, revoked_at = $2, replaced_by_token_id = $3
        where id = $1
    `, current.ID, now, nextID)
	if err != nil {
		return loginsvc.RotationResult{}, fmt.Errorf("mark current api refresh token rotated: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return loginsvc.RotationResult{}, fmt.Errorf("commit rotate api refresh token: %w", err)
	}

	return loginsvc.RotationResult{
		UserID:     current.UserID,
		FamilyID:   current.FamilyID,
		Authorized: true,
	}, nil
}

// RevokeRefreshTokenByHash invalidates a single refresh token.
func (r *LoginRepository) RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, now time.Time) error {
	_, err := QuerierFromContext(ctx, r.db).Exec(ctx, `
        update api_refresh_tokens
        set revoked_at = coalesce(revoked_at, $2)
        where token_hash = $1
    `, strings.TrimSpace(tokenHash), now)
	if err != nil {
		return fmt.Errorf("revoke api refresh token: %w", err)
	}
	return nil
}

// RevokeRefreshTokenFamily invalidates every token descended from one login.
func (r *LoginRepository) RevokeRefreshTokenFamily(ctx context.Context, familyID string, now time.Time) error {
	_, err := QuerierFromContext(ctx, r.db).Exec(ctx, `
        update api_refresh_tokens
        set revoked_at = coalesce(revoked_at, $2)
        where family_id = $1
    `, strings.TrimSpace(familyID), now)
	if err != nil {
		return fmt.Errorf("revoke api refresh token family: %w", err)
	}
	return nil
}

// FindRefreshTokenByHash retrieves one stored refresh token, for tests and
// maintenance commands.
func (r *LoginRepository) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (loginsvc.RefreshToken, error) {
	var out loginsvc.RefreshToken
	err := QuerierFromContext(ctx, r.db).QueryRow(ctx, `
        select `+refreshTokenColumns+`
        from api_refresh_tokens
        where token_hash = $1
    `, strings.TrimSpace(tokenHash)).Scan(
		&out.ID, &out.UserID, &out.FamilyID, &out.TokenHash, &out.ExpiresAt,
		&out.CreatedAt, &out.LastUsedAt, &out.RevokedAt, &out.ReplacedByTokenID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return loginsvc.RefreshToken{}, ErrNoRows
		}
		return loginsvc.RefreshToken{}, fmt.Errorf("get api refresh token: %w", err)
	}
	return out, nil
}

// DeleteRefreshTokensBefore removes tokens that expired, or were revoked,
// before the cutoff. It returns how many rows were removed.
//
// Deleting a rotated predecessor is safe: replaced_by_token_id is set at the
// same moment as revoked_at, so reuse detection still catches a replay through
// the revoked_at check even after the pointer is gone.
func (r *LoginRepository) DeleteRefreshTokensBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := QuerierFromContext(ctx, r.db).Exec(ctx, `
        delete from api_refresh_tokens
        where expires_at < $1
           or (revoked_at is not null and revoked_at < $1)
    `, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired api refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}
