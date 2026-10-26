-- +goose Up
-- 当前玩家：多玩家账号此前建会话时硬取第一个 profile，用户无法选择用哪个名字进服。
-- 存在 users 上（每用户至多一个当前玩家，天然不存在并列/多个 active 的状态）。
ALTER TABLE users ADD COLUMN active_player_uuid TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN active_player_uuid;
