create table if not exists users (
    id bigint generated always as identity primary key,
    email text unique,
    display_name text not null,
    avatar_url text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists user_identities (
    id bigint generated always as identity primary key,
    user_id bigint not null references users(id) on delete cascade,
    provider text not null,
    provider_user_id text not null,
    provider_email text,
    provider_name text,
    provider_handle text,
    avatar_url text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (provider, provider_user_id)
);

create index if not exists idx_user_identities_user_id on user_identities(user_id);

create table if not exists user_sessions (
    id bigint generated always as identity primary key,
    user_id bigint not null references users(id) on delete cascade,
    token_hash text not null unique,
    expires_at timestamptz not null,
    created_at timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    ip text,
    user_agent text,
    revoked_at timestamptz
);

create index if not exists idx_user_sessions_user_id on user_sessions(user_id);
create index if not exists idx_user_sessions_expires_at on user_sessions(expires_at);

create table if not exists api_refresh_tokens (
    id bigint generated always as identity primary key,
    user_id bigint not null references users(id) on delete cascade,
    family_id text not null,
    token_hash text not null unique,
    expires_at timestamptz not null,
    created_at timestamptz not null default now(),
    last_used_at timestamptz,
    revoked_at timestamptz,
    replaced_by_token_id bigint references api_refresh_tokens(id) on delete set null
);

create index if not exists idx_api_refresh_tokens_user_id on api_refresh_tokens(user_id);
create index if not exists idx_api_refresh_tokens_family_id on api_refresh_tokens(family_id);
create index if not exists idx_api_refresh_tokens_expires_at on api_refresh_tokens(expires_at);

create or replace function set_updated_at()
returns trigger
language plpgsql
as $$
begin
    new.updated_at = now();
    return new;
end;
$$;

drop trigger if exists users_set_updated_at on users;
create trigger users_set_updated_at
before update on users
for each row
execute function set_updated_at();

drop trigger if exists user_identities_set_updated_at on user_identities;
create trigger user_identities_set_updated_at
before update on user_identities
for each row
execute function set_updated_at();
