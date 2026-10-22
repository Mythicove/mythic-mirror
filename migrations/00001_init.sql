-- +goose Up
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    nickname      TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'user',
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE players (
    uuid          UUID PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    model         TEXT NOT NULL DEFAULT 'default',
    skin_id       UUID,
    cape_id       UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE textures (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hash        TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL CHECK (type IN ('skin', 'cape')),
    width       INT NOT NULL,
    height      INT NOT NULL,
    size        BIGINT NOT NULL,
    storage_key TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    access_token  TEXT NOT NULL UNIQUE,
    client_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE textures;
DROP TABLE players;
DROP TABLE users;
