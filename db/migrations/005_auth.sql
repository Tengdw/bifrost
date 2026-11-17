create table bifrost.auth_admin (
    singleton boolean primary key default true check (singleton),
    user_id bytea not null unique check (octet_length(user_id) = 32),
    username text not null,
    password_hash bytea not null
);

create table bifrost.auth_passkeys (
    credential_id bytea primary key,
    user_id bytea not null,
    credential jsonb not null,
    created_at timestamptz not null default now(),
    last_used_at timestamptz
);

create table bifrost.auth_sessions (
    token_hash bytea primary key,
    user_id bytea not null,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);
create index auth_sessions_expiry_idx on bifrost.auth_sessions(expires_at);
