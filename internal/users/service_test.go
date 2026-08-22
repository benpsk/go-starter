package users

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/benpsk/go-starter/internal/apperror"
)

// repoStub is an in-memory Repository. It stands in for internal/postgres so
// the business rules can be exercised without a database; the repository's own
// SQL is covered by the integration tests in internal/postgres.
type repoStub struct {
	byID        map[int64]User
	byEmail     map[string]User
	byIdentity  map[string]User
	identities  map[int64][]Identity
	createErr   error
	updateCalls int
	created     SocialProfile
	nextID      int64
}

func newRepoStub() *repoStub {
	return &repoStub{
		byID:       map[int64]User{},
		byEmail:    map[string]User{},
		byIdentity: map[string]User{},
		identities: map[int64][]Identity{},
		nextID:     1,
	}
}

func (r *repoStub) FindByID(_ context.Context, id int64) (User, error) {
	if found, ok := r.byID[id]; ok {
		return found, nil
	}
	return User{}, ErrNotFound
}

func (r *repoStub) FindByEmail(_ context.Context, email string) (User, error) {
	if found, ok := r.byEmail[email]; ok {
		return found, nil
	}
	return User{}, ErrNotFound
}

func (r *repoStub) FindByIdentity(_ context.Context, provider, providerUserID string) (User, error) {
	if found, ok := r.byIdentity[provider+":"+providerUserID]; ok {
		return found, nil
	}
	return User{}, ErrNotFound
}

func (r *repoStub) ListIdentitiesByUserID(_ context.Context, userID int64) ([]Identity, error) {
	return r.identities[userID], nil
}

func (r *repoStub) CreateWithIdentity(_ context.Context, profile SocialProfile) (User, error) {
	if r.createErr != nil {
		return User{}, r.createErr
	}
	r.created = profile
	created := User{ID: r.nextID, Email: profile.Email, DisplayName: profile.Name}
	r.nextID++
	r.byID[created.ID] = created
	if created.Email != "" {
		r.byEmail[created.Email] = created
	}
	r.byIdentity[profile.Provider+":"+profile.ProviderUserID] = created
	return created, nil
}

func (r *repoStub) UpdateFromProfile(_ context.Context, _ int64, _ SocialProfile) error {
	r.updateCalls++
	return nil
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected an *apperror.AppError, got %T: %v", err, err)
	}
	return appErr.Status
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected an *apperror.AppError, got %T: %v", err, err)
	}
	return appErr.Code
}

func TestFindByIDTranslatesNotFound(t *testing.T) {
	svc := NewService(newRepoStub())

	if _, err := svc.FindByID(context.Background(), 42); statusOf(t, err) != http.StatusNotFound {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestResolveSocialProfileCreatesOnFirstSignIn(t *testing.T) {
	repo := newRepoStub()
	svc := NewService(repo)

	created, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-1",
		Email:          "new@example.com",
		EmailVerified:  true,
		Name:           "New User",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == 0 || created.DisplayName != "New User" {
		t.Fatalf("unexpected created user: %+v", created)
	}
	if repo.created.ProviderUserID != "g-1" {
		t.Fatalf("expected the profile to reach the repository: %+v", repo.created)
	}
}

func TestResolveSocialProfileReusesExistingIdentity(t *testing.T) {
	repo := newRepoStub()
	existing := User{ID: 7, Email: "old@example.com", DisplayName: "Old Name"}
	repo.byID[7] = existing
	repo.byIdentity["google:g-1"] = existing
	svc := NewService(repo)

	resolved, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-1",
		Name:           "Fresh Name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.ID != 7 {
		t.Fatalf("expected the existing account, got %+v", resolved)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("expected the stored profile to be refreshed once, got %d", repo.updateCalls)
	}
}

func TestResolveSocialProfileRejectsEmailOwnedByAnotherProvider(t *testing.T) {
	repo := newRepoStub()
	repo.byEmail["taken@example.com"] = User{ID: 3, Email: "taken@example.com"}
	svc := NewService(repo)

	_, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-9",
		Email:          "taken@example.com",
		EmailVerified:  true,
	})
	if statusOf(t, err) != http.StatusConflict {
		t.Fatalf("expected 409, got %v", err)
	}
	if codeOf(t, err) != "email_conflict" {
		t.Fatalf("expected the email_conflict code, got %v", err)
	}
}

func TestResolveSocialProfileAllowsUnverifiedDuplicateEmail(t *testing.T) {
	repo := newRepoStub()
	repo.byEmail["taken@example.com"] = User{ID: 3, Email: "taken@example.com"}
	svc := NewService(repo)

	// An unverified address proves nothing, so it must not block a sign-in or
	// silently merge two accounts.
	if _, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-9",
		Email:          "taken@example.com",
		EmailVerified:  false,
		Name:           "Someone",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveSocialProfileTranslatesRepositoryConflicts(t *testing.T) {
	cases := []struct {
		name     string
		repoErr  error
		wantCode string
	}{
		{"email conflict", ErrEmailConflict, "email_conflict"},
		{"identity conflict", ErrIdentityConflict, "identity_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepoStub()
			repo.createErr = tc.repoErr
			svc := NewService(repo)

			_, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{
				Provider:       "google",
				ProviderUserID: "g-2",
			})
			if statusOf(t, err) != http.StatusConflict {
				t.Fatalf("expected 409, got %v", err)
			}
			if codeOf(t, err) != tc.wantCode {
				t.Fatalf("expected code %q, got %v", tc.wantCode, err)
			}
		})
	}
}

func TestResolveSocialProfileRejectsIncompleteProfile(t *testing.T) {
	svc := NewService(newRepoStub())

	_, err := svc.ResolveSocialProfile(context.Background(), SocialProfile{Provider: "google"})
	if statusOf(t, err) != http.StatusBadRequest {
		t.Fatalf("expected 400, got %v", err)
	}
}

func TestSocialProfileValidate(t *testing.T) {
	cases := []struct {
		name    string
		profile SocialProfile
		wantErr bool
	}{
		{"complete", SocialProfile{Provider: "google", ProviderUserID: "1"}, false},
		{"missing provider", SocialProfile{ProviderUserID: "1"}, true},
		{"missing provider user id", SocialProfile{Provider: "google"}, true},
		{"blank provider user id", SocialProfile{Provider: "google", ProviderUserID: "   "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.profile.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFindByEmail(t *testing.T) {
	repo := newRepoStub()
	repo.byEmail["found@example.com"] = User{ID: 4, Email: "found@example.com"}
	svc := NewService(repo)

	found, err := svc.FindByEmail(context.Background(), "found@example.com")
	if err != nil || found.ID != 4 {
		t.Fatalf("FindByEmail = %+v, %v", found, err)
	}
	if _, err := svc.FindByEmail(context.Background(), "missing@example.com"); statusOf(t, err) != http.StatusNotFound {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestListIdentities(t *testing.T) {
	repo := newRepoStub()
	repo.identities[9] = []Identity{{ID: 1, UserID: 9, Provider: "google"}}
	svc := NewService(repo)

	identities, err := svc.ListIdentities(context.Background(), 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(identities) != 1 || identities[0].Provider != "google" {
		t.Fatalf("unexpected identities: %+v", identities)
	}
}
