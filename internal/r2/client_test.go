package r2

import "testing"

func TestGetPublicURLJoinsBaseAndKey(t *testing.T) {
	t.Parallel()

	client := &Client{publicBaseURL: "https://cdn.example.com/media/"}
	got := client.GetPublicURL("uploads/user/file.jpg")
	want := "https://cdn.example.com/media/uploads/user/file.jpg"

	if got != want {
		t.Fatalf("unexpected public url: got %q want %q", got, want)
	}
}

func TestGetPublicURLCleansDuplicateSlashes(t *testing.T) {
	t.Parallel()

	client := &Client{publicBaseURL: "https://cdn.example.com/media"}
	got := client.GetPublicURL("/uploads//user/file.jpg")
	want := "https://cdn.example.com/media/uploads/user/file.jpg"

	if got != want {
		t.Fatalf("unexpected public url: got %q want %q", got, want)
	}
}
