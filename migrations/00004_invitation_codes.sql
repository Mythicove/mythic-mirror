-- +goose Up
CREATE TABLE invitation_codes (
    code       TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE invitation_codes;
