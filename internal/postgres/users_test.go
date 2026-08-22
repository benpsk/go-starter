package postgres_test

import (
	"errors"
	"testing"

	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
	"github.com/benpsk/go-starter/internal/users"
)

func googleProfile() users.SocialProfile {
	return users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-1001",
		Email:          "alice@example.com",
		EmailVerified:  true,
		Name:           "Alice Example",
		AvatarURL:      "https://img.example.com/alice.png",
	}
}

func TestUserRepositoryCreateWithIdentity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)
	profile := googleProfile()

	created, err := repo.CreateWithIdentity(ctx, profile)
	if err != nil {
		t.Fatalf("CreateWithIdentity failed: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected an assigned id, got %+v", created)
	}
	if created.Email != profile.Email || created.DisplayName != profile.Name {
		t.Fatalf("unexpected created user: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be populated: %+v", created)
	}

	t.Run("is findable by id, email, and identity", func(t *testing.T) {
		byID, err := repo.FindByID(ctx, created.ID)
		if err != nil || byID.ID != created.ID {
			t.Fatalf("FindByID = %+v, %v", byID, err)
		}
		byEmail, err := repo.FindByEmail(ctx, "ALICE@example.com")
		if err != nil || byEmail.ID != created.ID {
			t.Fatalf("FindByEmail must be case insensitive: %+v, %v", byEmail, err)
		}
		byIdentity, err := repo.FindByIdentity(ctx, "GOOGLE", profile.ProviderUserID)
		if err != nil || byIdentity.ID != created.ID {
			t.Fatalf("FindByIdentity must normalise the provider: %+v, %v", byIdentity, err)
		}
	})

	t.Run("links exactly one identity", func(t *testing.T) {
		identities, err := repo.ListIdentitiesByUserID(ctx, created.ID)
		if err != nil {
			t.Fatalf("ListIdentitiesByUserID failed: %v", err)
		}
		if len(identities) != 1 {
			t.Fatalf("expected one identity, got %d", len(identities))
		}
		identity := identities[0]
		if identity.Provider != "google" || identity.ProviderUserID != profile.ProviderUserID {
			t.Fatalf("unexpected identity: %+v", identity)
		}
		if identity.ProviderEmail != profile.Email || identity.ProviderName != profile.Name {
			t.Fatalf("expected the provider profile to be stored: %+v", identity)
		}
	})
}

func TestUserRepositoryCreateWithIdentityRejectsDuplicateEmail(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)
	if _, err := repo.CreateWithIdentity(ctx, googleProfile()); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	second := googleProfile()
	second.Provider = "github"
	second.ProviderUserID = "gh-2002"

	if _, err := repo.CreateWithIdentity(ctx, second); !errors.Is(err, users.ErrEmailConflict) {
		t.Fatalf("expected ErrEmailConflict, got %v", err)
	}
}

func TestUserRepositoryCreateWithIdentityRejectsDuplicateIdentity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)
	if _, err := repo.CreateWithIdentity(ctx, googleProfile()); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Same provider account, different email: the identity uniqueness
	// constraint has to catch this.
	second := googleProfile()
	second.Email = "other@example.com"

	if _, err := repo.CreateWithIdentity(ctx, second); !errors.Is(err, users.ErrIdentityConflict) {
		t.Fatalf("expected ErrIdentityConflict, got %v", err)
	}
}

func TestUserRepositoryCreateWithIdentityAllowsMissingEmail(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)

	first, err := repo.CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-1",
		Username:       "octocat",
	})
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if first.DisplayName != "octocat" {
		t.Fatalf("expected the username to become the display name, got %q", first.DisplayName)
	}
	if first.Email != "" {
		t.Fatalf("expected no email, got %q", first.Email)
	}

	// A null email must not collide with another null email.
	if _, err := repo.CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "github",
		ProviderUserID: "gh-2",
	}); err != nil {
		t.Fatalf("a second emailless account must be allowed: %v", err)
	}
}

func TestUserRepositoryCreateWithIdentityValidatesProfile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)

	if _, err := repo.CreateWithIdentity(ctx, users.SocialProfile{Provider: "google"}); err == nil {
		t.Fatalf("expected an incomplete profile to be rejected")
	}
}

func TestUserRepositoryUpdateFromProfile(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)
	created, err := repo.CreateWithIdentity(ctx, googleProfile())
	if err != nil {
		t.Fatalf("CreateWithIdentity failed: %v", err)
	}

	t.Run("newer values overwrite", func(t *testing.T) {
		refreshed := googleProfile()
		refreshed.Name = "Alice Renamed"
		refreshed.AvatarURL = "https://img.example.com/new.png"

		if err := repo.UpdateFromProfile(ctx, created.ID, refreshed); err != nil {
			t.Fatalf("UpdateFromProfile failed: %v", err)
		}
		found, err := repo.FindByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("FindByID failed: %v", err)
		}
		if found.DisplayName != "Alice Renamed" || found.AvatarURL != refreshed.AvatarURL {
			t.Fatalf("expected the profile to be refreshed: %+v", found)
		}
	})

	t.Run("omitted values are preserved", func(t *testing.T) {
		// A provider that stops returning a name must not blank the stored one.
		sparse := users.SocialProfile{Provider: "google", ProviderUserID: googleProfile().ProviderUserID}

		if err := repo.UpdateFromProfile(ctx, created.ID, sparse); err != nil {
			t.Fatalf("UpdateFromProfile failed: %v", err)
		}
		found, err := repo.FindByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("FindByID failed: %v", err)
		}
		if found.DisplayName != "Alice Renamed" {
			t.Fatalf("expected the display name to survive an empty update, got %q", found.DisplayName)
		}
	})
}

func TestUserRepositoryLookupMisses(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	repo := postgres.NewUserRepository(pool)

	cases := []struct {
		name string
		call func() error
	}{
		{"by id", func() error { _, err := repo.FindByID(ctx, 999999); return err }},
		{"by email", func() error { _, err := repo.FindByEmail(ctx, "nobody@example.com"); return err }},
		{"by blank email", func() error { _, err := repo.FindByEmail(ctx, "   "); return err }},
		{"by identity", func() error { _, err := repo.FindByIdentity(ctx, "google", "nope"); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, users.ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestUserRepositoryListIdentitiesIsEmptyForUnknownUser(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	identities, err := postgres.NewUserRepository(pool).ListIdentitiesByUserID(ctx, 999999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identities == nil || len(identities) != 0 {
		t.Fatalf("expected an empty non-nil slice, got %#v", identities)
	}
}

// compile-time assertion that the repository satisfies the domain contract
var _ users.Repository = (*postgres.UserRepository)(nil)
