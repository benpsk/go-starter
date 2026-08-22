package components

import (
	"testing"

	"github.com/benpsk/go-starter/internal/config"
)

func props(appName, appURL, tagID string, meta Meta) PageProps {
	return PageProps{
		Setting: config.Setting{AppName: appName, AppURL: appURL, GoogleTagID: tagID},
		Meta:    meta,
	}
}

func TestFullTitle(t *testing.T) {
	cases := []struct {
		name    string
		appName string
		title   string
		want    string
	}{
		{"scoped by app name", "Go Starter", "About", "About | Go Starter"},
		{"title only", "", "About", "About"},
		{"app name only", "Go Starter", "", "Go Starter"},
		{"no duplicate when equal", "Go Starter", "Go Starter", "Go Starter"},
		{"case insensitive duplicate", "Go Starter", "go starter", "go starter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := props(tc.appName, "", "", Meta{Title: tc.title}).FullTitle()
			if got != tc.want {
				t.Fatalf("FullTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCanonicalURL(t *testing.T) {
	cases := []struct {
		name   string
		appURL string
		path   string
		want   string
	}{
		{"root", "https://app.example.com", "/", "https://app.example.com/"},
		{"empty path is root", "https://app.example.com", "", "https://app.example.com/"},
		{"nested", "https://app.example.com", "/about", "https://app.example.com/about"},
		{"trailing slash on base", "https://app.example.com/", "/about", "https://app.example.com/about"},
		{"missing leading slash", "https://app.example.com", "about", "https://app.example.com/about"},
		{"blank base falls back", "", "/about", "http://127.0.0.1:8080/about"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := props("", tc.appURL, "", Meta{Path: tc.path}).canonicalURL()
			if got != tc.want {
				t.Fatalf("canonicalURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvedType(t *testing.T) {
	if got := props("", "", "", Meta{}).resolvedType(); got != "website" {
		t.Fatalf("expected the website default, got %q", got)
	}
	if got := props("", "", "", Meta{Type: "article"}).resolvedType(); got != "article" {
		t.Fatalf("expected article, got %q", got)
	}
}

func TestAnalytics(t *testing.T) {
	off := props("", "", "  ", Meta{})
	if off.analyticsEnabled() {
		t.Fatalf("a blank tag id must not enable analytics")
	}

	on := props("", "", "G-ABC123", Meta{})
	if !on.analyticsEnabled() {
		t.Fatalf("expected analytics to be enabled")
	}
	if want := "https://www.googletagmanager.com/gtag/js?id=G-ABC123"; on.gtagSrc() != want {
		t.Fatalf("gtagSrc() = %q, want %q", on.gtagSrc(), want)
	}
}

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
