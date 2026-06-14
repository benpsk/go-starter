package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/user"
	"github.com/golang-jwt/jwt/v5"
)

type apiAccessClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

type ParsedAPIAccessToken struct {
	UserID    int64
	SessionID string
}

type APITokenResponse struct {
	TokenType             string    `json:"token_type"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
}

func (s *Service) IssueAPITokenPair(ctx context.Context, userID int64, now time.Time) (APITokenResponse, error) {
	familyID, err := randomToken(20)
	if err != nil {
		return APITokenResponse{}, err
	}
	refreshToken, err := randomToken(32)
	if err != nil {
		return APITokenResponse{}, err
	}
	refreshExpiresAt := now.Add(s.apiRefreshTokenTTL)
	if err := s.users.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: HashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}); err != nil {
		return APITokenResponse{}, err
	}

	accessToken, accessExpiresAt, err := s.IssueAPIAccessToken(userID, familyID, now)
	if err != nil {
		return APITokenResponse{}, err
	}

	return APITokenResponse{
		TokenType:             "bearer",
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func (s *Service) RotateAPIRefreshToken(ctx context.Context, currentRefreshToken string, now time.Time) (APITokenResponse, error) {
	currentHash := HashToken(currentRefreshToken)
	newRefreshToken, err := randomToken(32)
	if err != nil {
		return APITokenResponse{}, err
	}
	result, err := s.users.RotateAPIRefreshToken(ctx, currentHash, user.APIRefreshToken{
		TokenHash: HashToken(newRefreshToken),
		ExpiresAt: now.Add(s.apiRefreshTokenTTL),
	}, now)
	if err != nil {
		return APITokenResponse{}, err
	}
	if !result.Authorized {
		if result.ReuseDetected && result.FamilyID != "" {
			_ = s.users.RevokeAPIRefreshTokenFamily(ctx, result.FamilyID, now)
		}
		return APITokenResponse{}, errors.New("unauthorized")
	}

	accessToken, accessExpiresAt, err := s.IssueAPIAccessToken(result.UserID, result.FamilyID, now)
	if err != nil {
		return APITokenResponse{}, err
	}
	return APITokenResponse{
		TokenType:             "bearer",
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          newRefreshToken,
		RefreshTokenExpiresAt: now.Add(s.apiRefreshTokenTTL),
	}, nil
}

func (s *Service) IssueAPIAccessToken(userID int64, sessionID string, now time.Time) (string, time.Time, error) {
	if userID <= 0 || strings.TrimSpace(s.apiAccessTokenSecret) == "" {
		return "", time.Time{}, errors.New("api access token not configured")
	}
	if s.apiAccessTokenTTL <= 0 {
		s.apiAccessTokenTTL = 10 * time.Minute
	}
	expiresAt := now.Add(s.apiAccessTokenTTL)
	jti, err := randomToken(20)
	if err != nil {
		return "", time.Time{}, err
	}
	claims := apiAccessClaims{
		SessionID: strings.TrimSpace(sessionID),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   formatUserID(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    "go-starter",
			Audience:  []string{"go-starter-api"},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.apiAccessTokenSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (s *Service) ParseAPIAccessToken(tokenString string) (ParsedAPIAccessToken, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" || strings.TrimSpace(s.apiAccessTokenSecret) == "" {
		return ParsedAPIAccessToken{}, errors.New("unauthorized")
	}
	parsed, err := jwt.ParseWithClaims(tokenString, &apiAccessClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.apiAccessTokenSecret), nil
	})
	if err != nil {
		return ParsedAPIAccessToken{}, err
	}
	claims, ok := parsed.Claims.(*apiAccessClaims)
	if !ok || !parsed.Valid {
		return ParsedAPIAccessToken{}, errors.New("invalid token")
	}
	userID, err := parseUserID(claims.Subject)
	if err != nil {
		return ParsedAPIAccessToken{}, err
	}
	return ParsedAPIAccessToken{
		UserID:    userID,
		SessionID: strings.TrimSpace(claims.SessionID),
	}, nil
}

func BearerTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return ""
	}
	if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(authHeader[7:])
}

func (s *Service) APIRefreshTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if c, err := r.Cookie(s.apiRefreshCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func (s *Service) SetAPIRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.apiRefreshCookieName,
		Value:    token,
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SessionCookieSecure(r),
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func (s *Service) ClearAPIRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.apiRefreshCookieName,
		Value:    "",
		Path:     "/api/auth",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.SessionCookieSecure(r),
		MaxAge:   -1,
	})
}
