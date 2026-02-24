package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/benpsk/go-starter/internal/user"
)

func TestUserAuthStoreRotateAPIRefreshToken(t *testing.T) {
	ctx := context.Background()

	store := NewUserAuthStore(integrationPool)
	testUser := createTestUser(t, ctx, store)
	now := time.Now()

	t.Run("success rotation", func(t *testing.T) {
		oldRaw := uniqueRefreshRaw("refresh-old-success")
		newRaw := uniqueRefreshRaw("refresh-new-success")
		err := store.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
			UserID:    testUser.ID,
			FamilyID:  "family-success",
			TokenHash: hashForRefreshTest(oldRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create refresh token: %v", err)
		}

		result, err := store.RotateAPIRefreshToken(ctx, hashForRefreshTest(oldRaw), user.APIRefreshToken{
			TokenHash: hashForRefreshTest(newRaw),
			ExpiresAt: now.Add(48 * time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("rotate refresh token: %v", err)
		}
		if !result.Authorized || result.ReuseDetected {
			t.Fatalf("unexpected rotate result: %+v", result)
		}
		if result.UserID != testUser.ID || result.FamilyID != "family-success" {
			t.Fatalf("unexpected result values: %+v", result)
		}

		oldRow, err := store.GetAPIRefreshTokenByHash(ctx, hashForRefreshTest(oldRaw))
		if err != nil {
			t.Fatalf("load old refresh token: %v", err)
		}
		if oldRow.RevokedAt == nil || oldRow.ReplacedByTokenID == nil || oldRow.LastUsedAt == nil {
			t.Fatalf("expected old token to be marked rotated: %+v", oldRow)
		}

		newRow, err := store.GetAPIRefreshTokenByHash(ctx, hashForRefreshTest(newRaw))
		if err != nil {
			t.Fatalf("load new refresh token: %v", err)
		}
		if newRow.FamilyID != "family-success" || newRow.UserID != testUser.ID {
			t.Fatalf("unexpected new token row: %+v", newRow)
		}
	})

	t.Run("expired token marks family revoked and unauthorized", func(t *testing.T) {
		oldRaw := uniqueRefreshRaw("refresh-old-expired")
		newRaw := uniqueRefreshRaw("refresh-new-expired")
		err := store.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
			UserID:    testUser.ID,
			FamilyID:  "family-expired",
			TokenHash: hashForRefreshTest(oldRaw),
			ExpiresAt: now.Add(-1 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create expired refresh token: %v", err)
		}

		result, err := store.RotateAPIRefreshToken(ctx, hashForRefreshTest(oldRaw), user.APIRefreshToken{
			TokenHash: hashForRefreshTest(newRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("rotate expired refresh token: %v", err)
		}
		if result.Authorized || !result.ReuseDetected || result.FamilyID != "family-expired" {
			t.Fatalf("unexpected result for expired token: %+v", result)
		}

		oldRow, err := store.GetAPIRefreshTokenByHash(ctx, hashForRefreshTest(oldRaw))
		if err != nil {
			t.Fatalf("load expired token row: %v", err)
		}
		if oldRow.RevokedAt == nil {
			t.Fatalf("expected expired token family to be revoked")
		}
	})

	t.Run("already revoked token returns unauthorized reuse", func(t *testing.T) {
		oldRaw := uniqueRefreshRaw("refresh-old-revoked")
		newRaw := uniqueRefreshRaw("refresh-new-revoked")
		err := store.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
			UserID:    testUser.ID,
			FamilyID:  "family-revoked",
			TokenHash: hashForRefreshTest(oldRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create revoked refresh token: %v", err)
		}
		_, err = DBFromContext(ctx, integrationPool).Exec(ctx, `
			update api_refresh_tokens
			set revoked_at = now()
			where token_hash = $1
		`, hashForRefreshTest(oldRaw))
		if err != nil {
			t.Fatalf("mark revoked token: %v", err)
		}

		result, err := store.RotateAPIRefreshToken(ctx, hashForRefreshTest(oldRaw), user.APIRefreshToken{
			TokenHash: hashForRefreshTest(newRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("rotate revoked refresh token: %v", err)
		}
		if result.Authorized || !result.ReuseDetected || result.FamilyID != "family-revoked" {
			t.Fatalf("unexpected result for revoked token: %+v", result)
		}
	})

	t.Run("already replaced token returns unauthorized reuse", func(t *testing.T) {
		oldRaw := uniqueRefreshRaw("refresh-old-replaced")
		newRaw := uniqueRefreshRaw("refresh-new-replaced")
		newRaw2 := uniqueRefreshRaw("refresh-new-replaced-2")
		err := store.CreateAPIRefreshToken(ctx, user.APIRefreshToken{
			UserID:    testUser.ID,
			FamilyID:  "family-replaced",
			TokenHash: hashForRefreshTest(oldRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("create old token: %v", err)
		}
		result1, err := store.RotateAPIRefreshToken(ctx, hashForRefreshTest(oldRaw), user.APIRefreshToken{
			TokenHash: hashForRefreshTest(newRaw),
			ExpiresAt: now.Add(24 * time.Hour),
		}, now)
		if err != nil {
			t.Fatalf("first rotate: %v", err)
		}
		if !result1.Authorized {
			t.Fatalf("expected first rotate to authorize")
		}

		result2, err := store.RotateAPIRefreshToken(ctx, hashForRefreshTest(oldRaw), user.APIRefreshToken{
			TokenHash: hashForRefreshTest(newRaw2),
			ExpiresAt: now.Add(24 * time.Hour),
		}, now.Add(1*time.Minute))
		if err != nil {
			t.Fatalf("second rotate with old token: %v", err)
		}
		if result2.Authorized || !result2.ReuseDetected || result2.FamilyID != "family-replaced" {
			t.Fatalf("unexpected result for replaced token reuse: %+v", result2)
		}
	})
}

func createTestUser(t *testing.T, ctx context.Context, store *UserAuthStore) user.User {
	t.Helper()
	suffix := time.Now().UnixNano()
	u, err := store.CreateUserWithIdentity(ctx, user.SocialProfile{
		Provider:       "google",
		ProviderUserID: "store-test-" + strconv.FormatInt(suffix, 10),
		Email:          "store-test+" + strconv.FormatInt(suffix, 10) + "@example.com",
		EmailVerified:  true,
		Name:           "Store Test User",
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return u
}

func hashForRefreshTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func uniqueRefreshRaw(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
