package components

import "strings"

type PageMeta struct {
	Title       string
	Description string
	Keywords    string
	Path        string
	Type        string
}

func (m PageMeta) canonicalURL(appURL string) string {
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	path := normalizePath(m.Path)
	if path == "/" {
		return base + "/"
	}
	return base + path
}

func (m PageMeta) resolvedType() string {
	if strings.TrimSpace(m.Type) == "" {
		return "website"
	}
	return strings.TrimSpace(m.Type)
}

func (m PageMeta) fullTitle(appName string) string {
	title := strings.TrimSpace(m.Title)
	name := strings.TrimSpace(appName)
	if title == "" {
		return name
	}
	if name == "" || strings.EqualFold(title, name) {
		return title
	}
	return title + " | " + name
}

func normalizePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || p == "/" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func hasGoogleTagID(v string) bool {
	return strings.TrimSpace(v) != ""
}
