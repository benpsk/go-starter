package login

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
	"github.com/golang-jwt/jwt/v5"
)

const (
	userAgent       = "go-starter"
	tokenIssuer     = "go-starter"
	tokenAudience   = "go-starter-api"
	tokenTypeBearer = "bearer"
)

// TokenPair is the credential set returned to an API client after sign-in.
type TokenPair struct {
	TokenType             string    `json:"token_type"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
}

// RefreshToken is a stored refresh credential. Only its hash is persisted.
type RefreshToken struct {
	ID                int64
	UserID            int64
	FamilyID          string
	TokenHash         string
	ExpiresAt         time.Time
	CreatedAt         time.Time
	LastUsedAt        *time.Time
	RevokedAt         *time.Time
	ReplacedByTokenID *int64
}

// RotationResult reports the outcome of a refresh token rotation attempt.
type RotationResult struct {
	UserID        int64
	FamilyID      string
	ReuseDetected bool
	Authorized    bool
}

// AccessTokenClaims is the validated payload of a bearer access token.
type AccessTokenClaims struct {
	UserID   int64
	FamilyID string
}

type accessClaims struct {
	FamilyID string `json:"fid"`
	jwt.RegisteredClaims
}

// IssueTokenPair mints a new refresh token family plus its first access token.
func (s *service) IssueTokenPair(ctx context.Context, userID int64, now time.Time) (TokenPair, error) {
	if !s.APITokensConfigured() {
		return TokenPair{}, apperror.Unavailable("api auth is not configured")
	}
	familyID, err := randomToken(20)
	if err != nil {
		return TokenPair{}, apperror.Internal("failed to generate token family", err)
	}
	refreshToken, err := randomToken(32)
	if err != nil {
		return TokenPair{}, apperror.Internal("failed to generate refresh token", err)
	}
	refreshExpiresAt := now.Add(s.cfg.RefreshTokenTTL)
	if err := s.repo.CreateRefreshToken(ctx, RefreshToken{
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: HashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}); err != nil {
		return TokenPair{}, apperror.Internal("failed to store refresh token", err)
	}

	accessToken, accessExpiresAt, err := s.signAccessToken(userID, familyID, now)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		TokenType:             tokenTypeBearer,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

// RotateRefreshToken swaps a refresh token for a fresh pair. Replaying an
// already-rotated token revokes the whole family, because a replay means the
// token leaked.
func (s *service) RotateRefreshToken(ctx context.Context, refreshToken string, now time.Time) (TokenPair, error) {
	if !s.APITokensConfigured() {
		return TokenPair{}, apperror.Unavailable("api auth is not configured")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return TokenPair{}, apperror.BadRequest("refresh_token is required")
	}
	nextToken, err := randomToken(32)
	if err != nil {
		return TokenPair{}, apperror.Internal("failed to generate refresh token", err)
	}
	refreshExpiresAt := now.Add(s.cfg.RefreshTokenTTL)
	result, err := s.repo.RotateRefreshToken(ctx, HashToken(refreshToken), RefreshToken{
		TokenHash: HashToken(nextToken),
		ExpiresAt: refreshExpiresAt,
	}, now)
	if err != nil {
		return TokenPair{}, apperror.Internal("failed to rotate refresh token", err)
	}
	if !result.Authorized {
		if result.ReuseDetected && result.FamilyID != "" {
			_ = s.repo.RevokeRefreshTokenFamily(ctx, result.FamilyID, now)
		}
		return TokenPair{}, apperror.Unauthorized("invalid refresh token")
	}

	accessToken, accessExpiresAt, err := s.signAccessToken(result.UserID, result.FamilyID, now)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		TokenType:             tokenTypeBearer,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          nextToken,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

// RevokeRefreshToken invalidates one refresh token, ignoring unknown values so
// logout stays idempotent.
func (s *service) RevokeRefreshToken(ctx context.Context, refreshToken string, now time.Time) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	if err := s.repo.RevokeRefreshTokenByHash(ctx, HashToken(refreshToken), now); err != nil {
		return apperror.Internal("failed to revoke refresh token", err)
	}
	return nil
}

// PruneRefreshTokens deletes refresh tokens whose usefulness has lapsed. The
// grace period keeps recently revoked rows around long enough to stay visible
// while an incident is being investigated.
func (s *service) PruneRefreshTokens(ctx context.Context, olderThan time.Duration, now time.Time) (int64, error) {
	if olderThan < 0 {
		return 0, apperror.BadRequest("grace period must not be negative")
	}
	removed, err := s.repo.DeleteRefreshTokensBefore(ctx, now.Add(-olderThan))
	if err != nil {
		return 0, apperror.Internal("failed to prune refresh tokens", err)
	}
	return removed, nil
}

// ParseAccessToken validates a bearer token's signature, audience, and expiry.
func (s *service) ParseAccessToken(token string) (AccessTokenClaims, error) {
	token = strings.TrimSpace(token)
	if token == "" || !s.APITokensConfigured() {
		return AccessTokenClaims{}, apperror.Unauthorized("unauthorized")
	}
	parsed, err := jwt.ParseWithClaims(token, &accessClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, apperror.Unauthorized("unexpected signing method")
		}
		return []byte(s.cfg.AccessTokenSecret), nil
	}, jwt.WithIssuer(tokenIssuer), jwt.WithAudience(tokenAudience))
	if err != nil {
		return AccessTokenClaims{}, apperror.Unauthorized("unauthorized")
	}
	claims, ok := parsed.Claims.(*accessClaims)
	if !ok || !parsed.Valid {
		return AccessTokenClaims{}, apperror.Unauthorized("unauthorized")
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(claims.Subject), 10, 64)
	if err != nil || userID <= 0 {
		return AccessTokenClaims{}, apperror.Unauthorized("unauthorized")
	}
	return AccessTokenClaims{UserID: userID, FamilyID: strings.TrimSpace(claims.FamilyID)}, nil
}

func (s *service) signAccessToken(userID int64, familyID string, now time.Time) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, apperror.Unauthorized("unauthorized")
	}
	expiresAt := now.Add(s.cfg.AccessTokenTTL)
	jti, err := randomToken(20)
	if err != nil {
		return "", time.Time{}, apperror.Internal("failed to generate token id", err)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		FamilyID: strings.TrimSpace(familyID),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    tokenIssuer,
			Audience:  []string{tokenAudience},
		},
	})
	signed, err := token.SignedString([]byte(s.cfg.AccessTokenSecret))
	if err != nil {
		return "", time.Time{}, apperror.Internal("failed to sign access token", err)
	}
	return signed, expiresAt, nil
}

// HashToken returns the stored representation of an opaque token. Raw tokens
// are never written to the database.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

// BearerTokenFromRequest extracts the credential from an Authorization header.
func BearerTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}
