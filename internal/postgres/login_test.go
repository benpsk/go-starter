package postgres_test

import (
	"testing"
	"time"

	loginsvc "github.com/benpsk/go-starter/internal/login"
	"github.com/benpsk/go-starter/internal/postgres"
	"github.com/benpsk/go-starter/internal/testutil"
	"github.com/benpsk/go-starter/internal/users"
)

func TestLoginRepositoryRotateRefreshToken(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-token-1",
		Email:          "token@example.com",
		Name:           "Token Owner",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	repo := postgres.NewLoginRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	if err := repo.CreateRefreshToken(ctx, loginsvc.RefreshToken{
		UserID:    owner.ID,
		FamilyID:  "family-1",
		TokenHash: loginsvc.HashToken("first"),
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}

	result, err := repo.RotateRefreshToken(ctx, loginsvc.HashToken("first"), loginsvc.RefreshToken{
		TokenHash: loginsvc.HashToken("second"),
		ExpiresAt: now.Add(2 * time.Hour),
	}, now)
	if err != nil {
		t.Fatalf("RotateRefreshToken failed: %v", err)
	}
	if !result.Authorized || result.UserID != owner.ID || result.FamilyID != "family-1" {
		t.Fatalf("unexpected rotation result: %+v", result)
	}

	t.Run("the old token is marked replaced", func(t *testing.T) {
		old, err := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken("first"))
		if err != nil {
			t.Fatalf("FindRefreshTokenByHash failed: %v", err)
		}
		if old.RevokedAt == nil || old.ReplacedByTokenID == nil {
			t.Fatalf("expected the rotated token to be revoked and linked: %+v", old)
		}
	})

	t.Run("the new token inherits the user and family", func(t *testing.T) {
		next, err := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken("second"))
		if err != nil {
			t.Fatalf("FindRefreshTokenByHash failed: %v", err)
		}
		if next.UserID != owner.ID || next.FamilyID != "family-1" {
			t.Fatalf("unexpected successor token: %+v", next)
		}
		if next.RevokedAt != nil {
			t.Fatalf("the successor must be live: %+v", next)
		}
	})
}

func TestLoginRepositoryRotateDetectsReuseAndRevokesFamily(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-token-2",
		Email:          "reuse@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	repo := postgres.NewLoginRepository(pool)
	now := time.Now().UTC()

	if err := repo.CreateRefreshToken(ctx, loginsvc.RefreshToken{
		UserID:    owner.ID,
		FamilyID:  "family-2",
		TokenHash: loginsvc.HashToken("first"),
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}
	if _, err := repo.RotateRefreshToken(ctx, loginsvc.HashToken("first"), loginsvc.RefreshToken{
		TokenHash: loginsvc.HashToken("second"),
		ExpiresAt: now.Add(time.Hour),
	}, now); err != nil {
		t.Fatalf("first rotation failed: %v", err)
	}

	// Replaying the first token means it leaked, so every token in the family
	// must be revoked rather than only the replayed one.
	result, err := repo.RotateRefreshToken(ctx, loginsvc.HashToken("first"), loginsvc.RefreshToken{
		TokenHash: loginsvc.HashToken("third"),
		ExpiresAt: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatalf("replay rotation failed: %v", err)
	}
	if result.Authorized {
		t.Fatalf("a replayed token must not be authorized: %+v", result)
	}
	if !result.ReuseDetected || result.FamilyID != "family-2" {
		t.Fatalf("expected reuse to be reported with the family: %+v", result)
	}

	for _, name := range []string{"first", "second"} {
		token, err := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken(name))
		if err != nil {
			t.Fatalf("FindRefreshTokenByHash(%s) failed: %v", name, err)
		}
		if token.RevokedAt == nil {
			t.Fatalf("expected %s to be revoked after reuse: %+v", name, token)
		}
	}
	if _, err := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken("third")); err == nil {
		t.Fatalf("a replayed rotation must not mint a successor")
	}
}

func TestLoginRepositoryRotateRejectsUnknownAndExpired(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-token-3",
		Email:          "expired@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	repo := postgres.NewLoginRepository(pool)
	now := time.Now().UTC()

	t.Run("unknown token", func(t *testing.T) {
		result, err := repo.RotateRefreshToken(ctx, loginsvc.HashToken("nope"), loginsvc.RefreshToken{
			TokenHash: loginsvc.HashToken("next"),
			ExpiresAt: now.Add(time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Authorized || result.ReuseDetected {
			t.Fatalf("an unknown token is neither authorized nor a reuse: %+v", result)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		if err := repo.CreateRefreshToken(ctx, loginsvc.RefreshToken{
			UserID:    owner.ID,
			FamilyID:  "family-3",
			TokenHash: loginsvc.HashToken("stale"),
			ExpiresAt: now.Add(-time.Hour),
		}); err != nil {
			t.Fatalf("CreateRefreshToken failed: %v", err)
		}

		result, err := repo.RotateRefreshToken(ctx, loginsvc.HashToken("stale"), loginsvc.RefreshToken{
			TokenHash: loginsvc.HashToken("after-stale"),
			ExpiresAt: now.Add(time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Authorized {
			t.Fatalf("an expired token must not rotate: %+v", result)
		}
	})
}

func TestLoginRepositoryRevoke(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx, done := testutil.WithTx(t, pool)
	defer done()

	owner, err := postgres.NewUserRepository(pool).CreateWithIdentity(ctx, users.SocialProfile{
		Provider:       "google",
		ProviderUserID: "g-token-4",
		Email:          "revoke@example.com",
	})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	repo := postgres.NewLoginRepository(pool)
	now := time.Now().UTC()

	for _, name := range []string{"a", "b"} {
		if err := repo.CreateRefreshToken(ctx, loginsvc.RefreshToken{
			UserID:    owner.ID,
			FamilyID:  "family-4",
			TokenHash: loginsvc.HashToken(name),
			ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateRefreshToken(%s) failed: %v", name, err)
		}
	}

	t.Run("by hash revokes one token", func(t *testing.T) {
		if err := repo.RevokeRefreshTokenByHash(ctx, loginsvc.HashToken("a"), now); err != nil {
			t.Fatalf("RevokeRefreshTokenByHash failed: %v", err)
		}
		revoked, _ := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken("a"))
		if revoked.RevokedAt == nil {
			t.Fatalf("expected token a to be revoked")
		}
		live, _ := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken("b"))
		if live.RevokedAt != nil {
			t.Fatalf("token b must stay live")
		}
	})

	t.Run("by family revokes the rest", func(t *testing.T) {
		if err := repo.RevokeRefreshTokenFamily(ctx, "family-4", now); err != nil {
			t.Fatalf("RevokeRefreshTokenFamily failed: %v", err)
		}
		for _, name := range []string{"a", "b"} {
			token, _ := repo.FindRefreshTokenByHash(ctx, loginsvc.HashToken(name))
			if token.RevokedAt == nil {
				t.Fatalf("expected %s to be revoked", name)
			}
		}
	})

	t.Run("revoking an unknown hash is not an error", func(t *testing.T) {
		if err := repo.RevokeRefreshTokenByHash(ctx, loginsvc.HashToken("missing"), now); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// compile-time assertion that the repository satisfies the domain contract
var _ loginsvc.Repository = (*postgres.LoginRepository)(nil)
