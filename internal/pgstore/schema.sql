CREATE TABLE users (
    id             TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    nickname       TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'user',
    status         TEXT NOT NULL DEFAULT 'active',
    email_verified BOOLEAN NOT NULL DEFAULT false,
    storage_tier   TEXT NOT NULL DEFAULT 'free',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE files (
    key           TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE players (
    uuid          TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    model         TEXT NOT NULL DEFAULT 'default',
    skin_key      TEXT NOT NULL DEFAULT '',
    cape_key      TEXT NOT NULL DEFAULT '',
    premium_uuid  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    player_id     TEXT NOT NULL,
    access_token  TEXT NOT NULL UNIQUE,
    client_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE server_binds (
    server_id  TEXT PRIMARY KEY,
    player_id  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE verification_tokens (
    token      TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE invitation_codes (
    code       TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE skinlib_items (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    player_name   TEXT NOT NULL,
    texture_key   TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT 'default',
    likes         INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE skinlib_comments (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    item_id    TEXT NOT NULL REFERENCES skinlib_items(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value JSONB NOT NULL
);

CREATE TABLE audit_log (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    actor      TEXT NOT NULL,
    actor_ip   TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC);

CREATE TABLE oidc_sessions (
    kind       TEXT NOT NULL,
    signature  TEXT NOT NULL,
    request_id TEXT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    data       JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, signature)
);
CREATE INDEX oidc_sessions_request_idx ON oidc_sessions (kind, request_id);
CREATE INDEX oidc_sessions_expires_idx ON oidc_sessions (expires_at);
