package components

import (
	"strings"

	"github.com/benpsk/go-starter/internal/config"
)

// Meta captures the per-page metadata rendered into the shared layout.
type Meta struct {
	Title       string
	Description string
	Keywords    string
	Path        string
	Type        string
}

// HeaderAuthProps drives the authenticated state in the site header.
type HeaderAuthProps struct {
	IsAuthenticated bool
	DisplayName     string
	AvatarURL       string
}

// PageProps is the base every server-rendered page embeds. Pages add their own
// fields on top of it rather than taking a widening list of arguments.
type PageProps struct {
	Setting config.Setting
	Meta    Meta
	Auth    HeaderAuthProps
}

func (p PageProps) canonicalURL() string {
	base := strings.TrimRight(strings.TrimSpace(p.Setting.AppURL), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	path := normalizePath(p.Meta.Path)
	if path == "/" {
		return base + "/"
	}
	return base + path
}

func (p PageProps) resolvedType() string {
	if value := strings.TrimSpace(p.Meta.Type); value != "" {
		return value
	}
	return "website"
}

// FullTitle is the document title: the page title scoped by the app name.
func (p PageProps) FullTitle() string {
	title := strings.TrimSpace(p.Meta.Title)
	name := strings.TrimSpace(p.Setting.AppName)
	if title == "" {
		return name
	}
	if name == "" || strings.EqualFold(title, name) {
		return title
	}
	return title + " | " + name
}

func (p PageProps) googleTagID() string {
	return strings.TrimSpace(p.Setting.GoogleTagID)
}

func (p PageProps) analyticsEnabled() bool {
	return p.googleTagID() != ""
}

func (p PageProps) gtagSrc() string {
	return "https://www.googletagmanager.com/gtag/js?id=" + p.googleTagID()
}

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed
}
