-- +goose Up
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value JSONB NOT NULL
);

-- +goose Down
DROP TABLE settings;
