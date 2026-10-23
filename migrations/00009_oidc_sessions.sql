-- +goose Up
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

-- +goose Down
DROP TABLE oidc_sessions;
