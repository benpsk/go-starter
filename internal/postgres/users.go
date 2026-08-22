package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/benpsk/go-starter/internal/users"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// UserRepository provides Postgres-backed user and identity access.
type UserRepository struct {
	db Querier
}

// NewUserRepository constructs a user repository instance.
func NewUserRepository(db Querier) *UserRepository {
	return &UserRepository{db: db}
}

const userColumns = `id, coalesce(email, ''), display_name, coalesce(avatar_url, ''), created_at, updated_at`

// FindByID retrieves one user by primary key.
func (r *UserRepository) FindByID(ctx context.Context, id int64) (users.User, error) {
	var out users.User
	err := QuerierFromContext(ctx, r.db).QueryRow(ctx, `
        select `+userColumns+`
        from users
        where id = $1
    `, id).Scan(&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, users.ErrNotFound
		}
		return users.User{}, fmt.Errorf("find user by id: %w", err)
	}
	return out, nil
}

// FindByEmail retrieves one user by canonical email address.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (users.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return users.User{}, users.ErrNotFound
	}
	var out users.User
	err := QuerierFromContext(ctx, r.db).QueryRow(ctx, `
        select `+userColumns+`
        from users
        where email = $1
    `, email).Scan(&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, users.ErrNotFound
		}
		return users.User{}, fmt.Errorf("find user by email: %w", err)
	}
	return out, nil
}

// FindByIdentity retrieves the user behind one external provider account.
func (r *UserRepository) FindByIdentity(ctx context.Context, provider, providerUserID string) (users.User, error) {
	var out users.User
	err := QuerierFromContext(ctx, r.db).QueryRow(ctx, `
        select u.id, coalesce(u.email, ''), u.display_name, coalesce(u.avatar_url, ''), u.created_at, u.updated_at
        from user_identities ui
        join users u on u.id = ui.user_id
        where ui.provider = $1 and ui.provider_user_id = $2
    `, strings.TrimSpace(strings.ToLower(provider)), strings.TrimSpace(providerUserID)).Scan(
		&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, users.ErrNotFound
		}
		return users.User{}, fmt.Errorf("find user by identity: %w", err)
	}
	return out, nil
}

// ListIdentitiesByUserID retrieves every provider linked to one account.
func (r *UserRepository) ListIdentitiesByUserID(ctx context.Context, userID int64) ([]users.Identity, error) {
	rows, err := QuerierFromContext(ctx, r.db).Query(ctx, `
        select
            id, user_id, provider, provider_user_id,
            coalesce(provider_email, ''), coalesce(provider_name, ''),
            coalesce(provider_handle, ''), coalesce(avatar_url, ''),
            created_at, updated_at
        from user_identities
        where user_id = $1
        order by provider asc
    `, userID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	defer rows.Close()

	identities := make([]users.Identity, 0)
	for rows.Next() {
		var item users.Identity
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Provider, &item.ProviderUserID,
			&item.ProviderEmail, &item.ProviderName, &item.ProviderHandle, &item.AvatarURL,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan identity: %w", err)
		}
		identities = append(identities, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identities: %w", err)
	}
	return identities, nil
}

// CreateWithIdentity inserts a user and its first linked identity atomically.
func (r *UserRepository) CreateWithIdentity(ctx context.Context, profile users.SocialProfile) (users.User, error) {
	if err := profile.Validate(); err != nil {
		return users.User{}, err
	}

	tx, err := QuerierFromContext(ctx, r.db).Begin(ctx)
	if err != nil {
		return users.User{}, fmt.Errorf("begin create user with identity: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck - safe to ignore rollback errors

	email := strings.TrimSpace(strings.ToLower(profile.Email))
	if email != "" {
		var existingID int64
		err = tx.QueryRow(ctx, `select id from users where email = $1`, email).Scan(&existingID)
		if err == nil && existingID > 0 {
			return users.User{}, users.ErrEmailConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return users.User{}, fmt.Errorf("check email conflict: %w", err)
		}
	}

	var out users.User
	err = tx.QueryRow(ctx, `
        insert into users (email, display_name, avatar_url)
        values (nullif($1, ''), $2, nullif($3, ''))
        returning `+userColumns+`
    `, email, displayNameFor(profile), strings.TrimSpace(profile.AvatarURL)).Scan(
		&out.ID, &out.Email, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return users.User{}, users.ErrEmailConflict
		}
		return users.User{}, fmt.Errorf("insert user: %w", err)
	}

	_, err = tx.Exec(ctx, `
        insert into user_identities (
            user_id, provider, provider_user_id, provider_email,
            provider_name, provider_handle, avatar_url
        ) values ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6, ''), nullif($7, ''))
    `, out.ID,
		strings.TrimSpace(strings.ToLower(profile.Provider)),
		strings.TrimSpace(profile.ProviderUserID),
		email,
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.Username),
		strings.TrimSpace(profile.AvatarURL),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return users.User{}, users.ErrIdentityConflict
		}
		return users.User{}, fmt.Errorf("insert identity: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return users.User{}, fmt.Errorf("commit create user with identity: %w", err)
	}
	return out, nil
}

// UpdateFromProfile refreshes the stored user and identity from a newer
// provider profile, leaving existing values in place when the profile omits
// them.
func (r *UserRepository) UpdateFromProfile(ctx context.Context, userID int64, profile users.SocialProfile) error {
	db := QuerierFromContext(ctx, r.db)
	_, err := db.Exec(ctx, `
        update users
        set display_name = coalesce(nullif($2, ''), display_name),
            avatar_url = coalesce(nullif($3, ''), avatar_url)
        where id = $1
    `, userID, strings.TrimSpace(profile.Name), strings.TrimSpace(profile.AvatarURL))
	if err != nil {
		return fmt.Errorf("update user from profile: %w", err)
	}

	_, err = db.Exec(ctx, `
        update user_identities
        set provider_email = nullif($3, ''),
            provider_name = nullif($4, ''),
            provider_handle = nullif($5, ''),
            avatar_url = nullif($6, '')
        where user_id = $1 and provider = $2
    `, userID,
		strings.TrimSpace(strings.ToLower(profile.Provider)),
		strings.TrimSpace(strings.ToLower(profile.Email)),
		strings.TrimSpace(profile.Name),
		strings.TrimSpace(profile.Username),
		strings.TrimSpace(profile.AvatarURL),
	)
	if err != nil {
		return fmt.Errorf("update identity from profile: %w", err)
	}
	return nil
}

func displayNameFor(profile users.SocialProfile) string {
	if name := strings.TrimSpace(profile.Name); name != "" {
		return name
	}
	if handle := strings.TrimSpace(profile.Username); handle != "" {
		return handle
	}
	return "User"
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
