package authctx

import (
	"context"
	"testing"
)

func TestUserRoundTrip(t *testing.T) {
	want := User{ID: 7, Email: "a@example.com", DisplayName: "A", AvatarURL: "https://img"}
	ctx := WithUser(context.Background(), want)

	got, ok := UserFromContext(ctx)
	if !ok {
		t.Fatalf("expected the principal to be present")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestUserFromEmptyContext(t *testing.T) {
	if _, ok := UserFromContext(context.Background()); ok {
		t.Fatalf("expected no principal on a bare context")
	}
}
