// Package users owns the user domain: the models, the business rules for
// resolving a social profile onto an account, and the persistence contract the
// postgres layer implements. It is transport-free and database-free.
package users

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/benpsk/go-starter/internal/apperror"
)

// Sentinel errors returned by Repository implementations. Services translate
// them into *apperror.AppError before they reach a transport.
var (
	ErrNotFound         = errors.New("user not found")
	ErrEmailConflict    = errors.New("email already exists")
	ErrIdentityConflict = errors.New("identity already exists")
)

// User is an account in this application.
type User struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email,omitempty"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Identity links a user to one external login provider.
type Identity struct {
	ID             int64     `json:"id"`
	UserID         int64     `json:"user_id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	ProviderEmail  string    `json:"provider_email,omitempty"`
	ProviderName   string    `json:"provider_name,omitempty"`
	ProviderHandle string    `json:"provider_handle,omitempty"`
	AvatarURL      string    `json:"avatar_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SocialProfile is the verified claim set an OAuth provider returned.
type SocialProfile struct {
	Provider       string
	ProviderUserID string
	Email          string
	EmailVerified  bool
	Name           string
	AvatarURL      string
	Username       string
}

// Validate reports whether the profile carries the fields an identity needs.
func (p SocialProfile) Validate() error {
	if strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.ProviderUserID) == "" {
		return errors.New("provider and provider user id are required")
	}
	return nil
}

// Service exposes user lookups and social account resolution.
type Service interface {
	FindByID(ctx context.Context, id int64) (User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
	ListIdentities(ctx context.Context, userID int64) ([]Identity, error)
	// ResolveSocialProfile returns the account behind a verified social
	// profile, creating it on first sign-in.
	ResolveSocialProfile(ctx context.Context, profile SocialProfile) (User, error)
}

// Repository abstracts persistence for users and their linked identities.
type Repository interface {
	FindByID(ctx context.Context, id int64) (User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByIdentity(ctx context.Context, provider, providerUserID string) (User, error)
	ListIdentitiesByUserID(ctx context.Context, userID int64) ([]Identity, error)
	CreateWithIdentity(ctx context.Context, profile SocialProfile) (User, error)
	UpdateFromProfile(ctx context.Context, userID int64, profile SocialProfile) error
}

type service struct {
	repo Repository
}

// NewService constructs a user service using the supplied repository.
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// FindByID returns one account by primary key.
func (s *service) FindByID(ctx context.Context, id int64) (User, error) {
	found, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return User{}, translate(err, "user not found", "failed to load user")
	}
	return found, nil
}

// FindByEmail returns one account by its canonical email address.
func (s *service) FindByEmail(ctx context.Context, email string) (User, error) {
	found, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		return User{}, translate(err, "user not found", "failed to load user")
	}
	return found, nil
}

// ListIdentities returns every login provider linked to the account.
func (s *service) ListIdentities(ctx context.Context, userID int64) ([]Identity, error) {
	identities, err := s.repo.ListIdentitiesByUserID(ctx, userID)
	if err != nil {
		return nil, apperror.Internal("failed to load linked providers", err)
	}
	return identities, nil
}

// ResolveSocialProfile links a verified provider profile to an account. An
// existing identity wins; otherwise a verified email that already belongs to
// another provider is rejected rather than silently merged, because this
// starter does not implement account linking.
func (s *service) ResolveSocialProfile(ctx context.Context, profile SocialProfile) (User, error) {
	if err := profile.Validate(); err != nil {
		return User{}, apperror.BadRequest("social profile is incomplete")
	}

	existing, err := s.repo.FindByIdentity(ctx, profile.Provider, profile.ProviderUserID)
	switch {
	case err == nil:
		if updateErr := s.repo.UpdateFromProfile(ctx, existing.ID, profile); updateErr != nil {
			return User{}, apperror.Internal("failed to refresh user profile", updateErr)
		}
		refreshed, refreshErr := s.repo.FindByID(ctx, existing.ID)
		if refreshErr != nil {
			return User{}, apperror.Internal("failed to reload user", refreshErr)
		}
		return refreshed, nil
	case errors.Is(err, ErrNotFound):
		// Fall through to account creation.
	default:
		return User{}, apperror.Internal("failed to resolve social identity", err)
	}

	if profile.EmailVerified && strings.TrimSpace(profile.Email) != "" {
		if _, err := s.repo.FindByEmail(ctx, profile.Email); err == nil {
			return User{}, emailConflict()
		} else if !errors.Is(err, ErrNotFound) {
			return User{}, apperror.Internal("failed to check email conflict", err)
		}
	}

	created, err := s.repo.CreateWithIdentity(ctx, profile)
	if err != nil {
		if errors.Is(err, ErrEmailConflict) {
			return User{}, emailConflict()
		}
		if errors.Is(err, ErrIdentityConflict) {
			return User{}, apperror.WithCode(
				apperror.Conflict("this provider account is already linked"),
				"identity_conflict",
			)
		}
		return User{}, apperror.Internal("failed to create user", err)
	}
	return created, nil
}

func emailConflict() *apperror.AppError {
	return apperror.WithCode(
		apperror.Conflict("account email is already used by another provider"),
		"email_conflict",
	)
}

func translate(err error, notFoundMessage, internalMessage string) error {
	if errors.Is(err, ErrNotFound) {
		return apperror.NotFound(notFoundMessage)
	}
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperror.New(http.StatusInternalServerError, internalMessage, err)
}
