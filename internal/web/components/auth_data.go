package components

// LoginProps drives the sign-in page.
type LoginProps struct {
	PageProps
	Error         string
	GoogleEnabled bool
	GitHubEnabled bool
}

// AnyProviderEnabled reports whether at least one provider can be offered.
func (p LoginProps) AnyProviderEnabled() bool {
	return p.GoogleEnabled || p.GitHubEnabled
}

// AccountUser is the narrow view of an account rendered on the account page.
type AccountUser struct {
	DisplayName string
	Email       string
	AvatarURL   string
}

// LinkedProvider is one external login shown on the account page.
type LinkedProvider struct {
	Provider string
	Handle   string
	Email    string
}

// AccountProps drives the account page.
type AccountProps struct {
	PageProps
	User      AccountUser
	Providers []LinkedProvider
}
