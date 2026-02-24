package pages

import (
	"github.com/benpsk/go-starter/internal/user"
	"github.com/benpsk/go-starter/internal/web/components"
)

type LoginPageModel struct {
	AppName       string
	AppURL        string
	GoogleTagID   string
	Auth          components.HeaderAuthData
	Error         string
	GoogleEnabled bool
	GitHubEnabled bool
}

type AccountPageModel struct {
	AppName     string
	AppURL      string
	GoogleTagID string
	Auth        components.HeaderAuthData
	User        user.User
	Identities  []user.Identity
}
