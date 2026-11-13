package web

import "github.com/benpsk/go-starter/internal/web/components"

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
	components.PageProps
	User      AccountUser
	Providers []LinkedProvider
}
