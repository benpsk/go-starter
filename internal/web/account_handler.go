package web

import (
	"net/http"

	"github.com/benpsk/go-starter/internal/authctx"
	"github.com/benpsk/go-starter/internal/config"
	usersvc "github.com/benpsk/go-starter/internal/users"
	"github.com/benpsk/go-starter/internal/web/components"
	"github.com/benpsk/go-starter/internal/web/pages"
)

// AccountHandler renders the signed-in account page.
type AccountHandler struct {
	users     usersvc.Service
	setting   config.Setting
	loginPath string
}

// NewAccountHandler wires the users service into a handler instance.
func NewAccountHandler(userService usersvc.Service, setting config.Setting, loginPath string) AccountHandler {
	return AccountHandler{users: userService, setting: setting, loginPath: loginPath}
}

// Show renders the account profile and its linked login providers.
func (h AccountHandler) Show(w http.ResponseWriter, r *http.Request) {
	principal, ok := authctx.UserFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, h.loginPath, http.StatusSeeOther)
		return
	}

	currentUser, err := h.users.FindByID(r.Context(), principal.ID)
	if err != nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}
	identities, err := h.users.ListIdentities(r.Context(), principal.ID)
	if err != nil {
		http.Error(w, "failed to load account", http.StatusInternalServerError)
		return
	}

	props := components.AccountProps{
		PageProps: pageProps(h.setting, r, components.Meta{
			Title:       "Account",
			Description: "Manage your account and linked login providers.",
			Keywords:    "account,profile,oauth",
			Path:        "/account",
		}),
		User:      toAccountUser(currentUser),
		Providers: toLinkedProviders(identities),
	}
	render(w, r, pages.AccountPage(props))
}

// toAccountUser narrows the domain record to what the page renders.
func toAccountUser(in usersvc.User) components.AccountUser {
	return components.AccountUser{
		DisplayName: in.DisplayName,
		Email:       in.Email,
		AvatarURL:   in.AvatarURL,
	}
}

func toLinkedProviders(in []usersvc.Identity) []components.LinkedProvider {
	out := make([]components.LinkedProvider, 0, len(in))
	for _, identity := range in {
		out = append(out, components.LinkedProvider{
			Provider: identity.Provider,
			Handle:   identity.ProviderHandle,
			Email:    identity.ProviderEmail,
		})
	}
	return out
}
