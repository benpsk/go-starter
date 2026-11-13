package web

import "github.com/benpsk/go-starter/internal/web/components"

// LoginProps drives the sign-in page.
type LoginProps struct {
	components.PageProps
	Error         string
	GoogleEnabled bool
	GitHubEnabled bool
}

// AnyProviderEnabled reports whether at least one provider can be offered.
func (p LoginProps) AnyProviderEnabled() bool {
	return p.GoogleEnabled || p.GitHubEnabled
}
