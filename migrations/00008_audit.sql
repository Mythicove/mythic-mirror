-- +goose Up
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

-- +goose Down
DROP TABLE audit_log;
