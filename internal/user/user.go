package user

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound         = errors.New("user not found")
	ErrEmailConflict    = errors.New("email already exists")
	ErrIdentityConflict = errors.New("identity already exists")
)

type User struct {
	ID          int64
	Email       string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Identity struct {
	ID             int64
	UserID         int64
	Provider       string
	ProviderUserID string
	ProviderEmail  string
	ProviderName   string
	ProviderHandle string
	AvatarURL      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SocialProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
	AvatarURL      string
	Username       string
}

func (p SocialProfile) Validate() error {
	if strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.ProviderUserID) == "" {
		return errors.New("provider and provider user id are required")
	}
	return nil
}

type Session struct {
	ID         int64
	UserID     int64
	TokenHash  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
	IP         string
	UserAgent  string
	RevokedAt  *time.Time
}

type APIRefreshToken struct {
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
