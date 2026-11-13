package web

import "testing"

func TestLoginPropsAnyProviderEnabled(t *testing.T) {
	cases := []struct {
		google, github, want bool
	}{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	}
	for _, tc := range cases {
		got := LoginProps{GoogleEnabled: tc.google, GitHubEnabled: tc.github}.AnyProviderEnabled()
		if got != tc.want {
			t.Fatalf("AnyProviderEnabled(google=%v, github=%v) = %v, want %v", tc.google, tc.github, got, tc.want)
		}
	}
}
