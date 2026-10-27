-- +goose Up
-- 启动器凭证与第三方绑定（ADR-0012）：网站主密码与启动器登录密码分离。
--
-- 启动器只认「玩家名 + 该玩家的独立密码」，主密码从此不进 Yggdrasil 协议端点；
-- 管理员因此可以在第三方登录可用后关掉主密码登录，而启动器照常工作。
-- 一个玩家可有多条独立密码（按设备/用途区分），撤销 = 删行，
-- 由它建立的会话经 sessions.credential_id 外键级联一并销毁。
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

-- 第三方身份绑定。此前 GitHub 登录只按 email 匹配且不留痕，
-- 「这个账号绑过第三方吗」根本无法判定；有了这张表，首次按 email 匹配到既有账号时
-- 写入绑定，之后登录只认绑定（不再单纯信任 provider 返回的 email）。
CREATE TABLE user_identities (
    provider   TEXT NOT NULL,
    subject    TEXT NOT NULL,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user ON user_identities (user_id);

-- 网站/OIDC 会话不由启动器凭证建立，此列为 NULL，player_id 为空串（不代表任何玩家）。
ALTER TABLE sessions ADD COLUMN credential_id TEXT REFERENCES player_credentials(id) ON DELETE CASCADE;
CREATE INDEX sessions_credential ON sessions (credential_id);

-- 「当前玩家」指针删除：启动器按玩家名直接定位玩家，网站侧不再需要全局当前玩家。
ALTER TABLE users DROP COLUMN active_player_uuid;

-- 只服务管理面板的「还有多少纯密码账号」计数（非登录热路径）。
CREATE INDEX users_password_only ON users (id) WHERE password_hash <> '';

-- +goose Down
DROP INDEX IF EXISTS users_password_only;
ALTER TABLE users ADD COLUMN active_player_uuid TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS sessions_credential;
ALTER TABLE sessions DROP COLUMN credential_id;
DROP TABLE IF EXISTS user_identities;
DROP TABLE IF EXISTS player_credentials;
