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

-- 只服务管理面板的「还有多少纯密码账号」计数（非登录热路径）。
CREATE INDEX users_password_only ON users (id) WHERE password_hash <> '';

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

-- 启动器独立密码（ADR-0012）：网站主密码不进 Yggdrasil 协议端点。
CREATE TABLE player_credentials (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    -- 玩家改名会重算 uuid（uuid 由名字派生），凭证跟随玩家本身而非旧 uuid。
    player_uuid   TEXT NOT NULL REFERENCES players(uuid) ON DELETE CASCADE ON UPDATE CASCADE,
    name          TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX player_credentials_player ON player_credentials (player_uuid);

-- 第三方身份绑定；「该账号是否绑过第三方」此前无从判定。
CREATE TABLE user_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities (user_id);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 空串=网站/OIDC 会话，不代表任何玩家。
    player_id     TEXT NOT NULL,
    access_token  TEXT NOT NULL UNIQUE,
    client_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    -- 建立本会话的启动器凭证；撤销凭证即级联销毁会话。网站/OIDC 会话为 NULL。
    credential_id TEXT REFERENCES player_credentials(id) ON DELETE CASCADE,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_credential ON sessions (credential_id);

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
    status        TEXT NOT NULL DEFAULT 'listed',
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

CREATE TABLE reports (
    id               TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    reporter_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    reason           TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending',
    reviewed_by      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at      TIMESTAMPTZ,
    UNIQUE (reporter_user_id, target_type, target_id)
);
CREATE INDEX idx_reports_status ON reports (status);
CREATE INDEX idx_skinlib_items_status ON skinlib_items (status);
CREATE INDEX oidc_sessions_expires_idx ON oidc_sessions (expires_at);
