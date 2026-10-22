CREATE TABLE users (
    id             TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT NOT NULL,
    nickname       TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'user',
    status         TEXT NOT NULL DEFAULT 'active',
    email_verified BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
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
