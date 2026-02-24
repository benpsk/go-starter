package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type apiAccessClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

type parsedAPIAccessToken struct {
	UserID    int64
	SessionID string
}

func (h handler) issueAPIAccessToken(userID int64, sessionID string, now time.Time) (string, time.Time, error) {
	if userID <= 0 || strings.TrimSpace(h.apiAccessTokenSecret) == "" {
		return "", time.Time{}, errors.New("api access token not configured")
	}
	if h.apiAccessTokenTTL <= 0 {
		h.apiAccessTokenTTL = 10 * time.Minute
	}
	expiresAt := now.Add(h.apiAccessTokenTTL)
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
	signed, err := token.SignedString([]byte(h.apiAccessTokenSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (h handler) parseAPIAccessToken(tokenString string) (parsedAPIAccessToken, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" || strings.TrimSpace(h.apiAccessTokenSecret) == "" {
		return parsedAPIAccessToken{}, errors.New("unauthorized")
	}
	parsed, err := jwt.ParseWithClaims(tokenString, &apiAccessClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(h.apiAccessTokenSecret), nil
	})
	if err != nil {
		return parsedAPIAccessToken{}, err
	}
	claims, ok := parsed.Claims.(*apiAccessClaims)
	if !ok || !parsed.Valid {
		return parsedAPIAccessToken{}, errors.New("invalid token")
	}
	userID, err := parseUserID(claims.Subject)
	if err != nil {
		return parsedAPIAccessToken{}, err
	}
	return parsedAPIAccessToken{
		UserID:    userID,
		SessionID: strings.TrimSpace(claims.SessionID),
	}, nil
}

func bearerTokenFromRequest(r *http.Request) string {
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
