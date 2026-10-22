-- +goose Up
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

-- +goose Down
DROP TABLE skinlib_comments;
DROP TABLE skinlib_items;
