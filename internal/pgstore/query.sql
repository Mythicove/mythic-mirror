-- name: GetUserAuth :one
SELECT id, password_hash FROM users WHERE email = $1;

-- name: GetPlayersByUser :many
SELECT uuid, name FROM players WHERE owner_user_id = $1 ORDER BY created_at;

-- name: CreatePlayer :one
INSERT INTO players (uuid, name, owner_user_id) VALUES ($1, $2, $3) RETURNING uuid, name;

-- name: CountPlayersByUser :one
SELECT count(*) FROM players WHERE owner_user_id = $1;

-- name: RenamePlayer :execrows
UPDATE players SET uuid = $2, name = $3 WHERE uuid = $1 AND owner_user_id = $4;

-- name: DeletePlayer :execrows
DELETE FROM players WHERE uuid = $1 AND owner_user_id = $2;

-- name: DeleteSessionsByPlayer :exec
DELETE FROM sessions WHERE player_id = $1;

-- name: DeleteServerBindsByPlayer :exec
DELETE FROM server_binds WHERE player_id = $1;

-- name: GetActivePlayerUUID :one
SELECT active_player_uuid FROM users WHERE id = $1;

-- name: SetActivePlayer :execrows
UPDATE users SET active_player_uuid = $2 WHERE id = $1;

-- name: CreateSession :exec
INSERT INTO sessions (user_id, player_id, access_token, client_token, refresh_token, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetSession :one
SELECT user_id, player_id, client_token, expires_at FROM sessions WHERE access_token = $1;

-- name: DeleteSessionByToken :execrows
DELETE FROM sessions WHERE access_token = $1;

-- name: DeleteSessionByTokenAndClient :execrows
DELETE FROM sessions WHERE access_token = $1 AND client_token = $2;

-- name: BindServer :exec
INSERT INTO server_binds (server_id, player_id) VALUES ($1, $2)
ON CONFLICT (server_id) DO UPDATE SET player_id = EXCLUDED.player_id, created_at = now();

-- name: HasJoined :one
SELECT p.uuid, p.name, p.model, p.skin_key, p.cape_key, p.premium_uuid, p.owner_user_id
FROM server_binds b JOIN players p ON p.uuid = b.player_id
WHERE b.server_id = $1 AND p.name = $2;

-- name: GetProfileByUUID :one
SELECT uuid, name, model, skin_key, cape_key, premium_uuid, owner_user_id FROM players WHERE uuid = $1;

-- name: GetProfileByName :one
SELECT uuid, name, model, skin_key, cape_key, premium_uuid, owner_user_id FROM players WHERE name = $1;

-- name: LookupNames :many
SELECT uuid, name FROM players WHERE name = ANY(@names::text[]);

-- name: SetSkin :exec
UPDATE players SET skin_key = $2, model = $3 WHERE uuid = $1;

-- name: SetCape :exec
UPDATE players SET cape_key = $2 WHERE uuid = $1;

-- name: SetPremium :exec
UPDATE players SET premium_uuid = $2 WHERE uuid = $1;

-- name: CreateUser :one
INSERT INTO users (email, password_hash, nickname) VALUES ($1, $2, $3) RETURNING id;

-- name: GetAccountByEmail :one
SELECT id, nickname, email_verified FROM users WHERE email = $1;

-- name: GetAccountByID :one
SELECT id, email, nickname, email_verified FROM users WHERE id = $1;

-- name: CreateVerificationToken :exec
INSERT INTO verification_tokens (token, user_id, kind, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetVerificationToken :one
SELECT user_id, expires_at FROM verification_tokens WHERE token = $1 AND kind = $2;

-- name: DeleteVerificationToken :exec
DELETE FROM verification_tokens WHERE token = $1;

-- name: MarkVerified :exec
UPDATE users SET email_verified = true WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: CreateInviteCode :exec
INSERT INTO invitation_codes (code) VALUES ($1);

-- name: DeleteInviteCode :execrows
DELETE FROM invitation_codes WHERE code = $1;

-- name: CreateSkinLibItem :one
INSERT INTO skinlib_items (owner_user_id, player_name, texture_key, description, model, status)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, owner_user_id, player_name, texture_key, description, model, likes, status, created_at;

-- name: SearchSkinLib :many
SELECT id, owner_user_id, player_name, texture_key, description, model, likes, status, created_at
FROM skinlib_items
WHERE status = 'listed'
  AND ($1::text = '' OR player_name ILIKE '%' || $1::text || '%' OR description ILIKE '%' || $1::text || '%')
ORDER BY (CASE WHEN $2::text = 'likes' THEN likes ELSE 0 END) DESC, created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountSkinLib :one
SELECT count(*) FROM skinlib_items
WHERE status = 'listed'
  AND ($1::text = '' OR player_name ILIKE '%' || $1::text || '%' OR description ILIKE '%' || $1::text || '%');

-- name: GetSkinLibItem :one
SELECT id, owner_user_id, player_name, texture_key, description, model, likes, status, created_at
FROM skinlib_items WHERE id = $1;

-- name: AdminSearchSkinLib :many
-- 管理端按状态列出（status 空=全部），最新在前。
SELECT id, owner_user_id, player_name, texture_key, description, model, likes, status, created_at
FROM skinlib_items
WHERE ($1::text = '' OR status = $1::text)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountAdminSkinLib :one
SELECT count(*) FROM skinlib_items WHERE ($1::text = '' OR status = $1::text);

-- name: SetSkinStatus :execrows
UPDATE skinlib_items SET status = $2 WHERE id = $1;

-- name: InsertReport :execrows
-- 举报（同一用户对同一目标唯一；0 行=重复举报）。
INSERT INTO reports (reporter_user_id, target_type, target_id, reason)
VALUES ($1, $2, $3, $4) ON CONFLICT (reporter_user_id, target_type, target_id) DO NOTHING;

-- name: ListReports :many
SELECT r.id, r.reporter_user_id, COALESCE(u.nickname, '')::text AS reporter_name,
       r.target_type, r.target_id, r.reason, r.status, r.reviewed_by,
       r.created_at, r.reviewed_at
FROM reports r LEFT JOIN users u ON u.id = r.reporter_user_id
WHERE ($1::text = '' OR r.status = $1::text)
ORDER BY (CASE WHEN r.status = 'pending' THEN 0 ELSE 1 END), r.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountReports :one
SELECT count(*) FROM reports WHERE ($1::text = '' OR status = $1::text);

-- name: ResolveReport :execrows
UPDATE reports SET status = $2, reviewed_by = $3, reviewed_at = now() WHERE id = $1;

-- name: LikeSkin :execrows
UPDATE skinlib_items SET likes = likes + 1 WHERE id = $1;

-- name: CreateSkinLibComment :exec
INSERT INTO skinlib_comments (item_id, user_id, content) VALUES ($1, $2, $3);

-- name: ListSkinLibComments :many
SELECT c.id, c.item_id, c.user_id, c.content, c.created_at,
       COALESCE(u.nickname, '')::text AS author_name
FROM skinlib_comments c LEFT JOIN users u ON u.id = c.user_id
WHERE c.item_id = $1 ORDER BY c.created_at ASC;

-- name: DeleteSkinLibComment :execrows
DELETE FROM skinlib_comments WHERE id = $1;

-- name: ListUsers :many
SELECT u.id, u.email, u.nickname, u.role, u.status, u.email_verified, u.created_at,
       COALESCE((SELECT string_agg(p.name, ',' ORDER BY p.created_at) FROM players p WHERE p.owner_user_id = u.id), '')::text AS player_names
FROM users u
WHERE ($1::text = '' OR u.email ILIKE '%' || $1::text || '%' OR u.nickname ILIKE '%' || $1::text || '%')
ORDER BY u.created_at DESC LIMIT $2 OFFSET $3;

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE ($1::text = '' OR email ILIKE '%' || $1::text || '%' OR nickname ILIKE '%' || $1::text || '%');

-- name: UpdateUserRoleStatus :exec
UPDATE users SET role = $2, status = $3 WHERE id = $1;

-- name: UserRole :one
SELECT role FROM users WHERE id = $1;

-- name: DeleteSkinLibItem :execrows
DELETE FROM skinlib_items WHERE id = $1;

-- name: SetSetting :exec
INSERT INTO settings (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;

-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;


-- name: WriteAudit :exec
INSERT INTO audit_log (actor, actor_ip, action, target, detail) VALUES ($1, $2, $3, $4, $5);

-- name: UserStorage :one
-- 用户存储 tier 与已用字节（按上传者归属的纹理累计，sha256 键去重）。
SELECT u.storage_tier AS tier,
       COALESCE((SELECT SUM(f.size_bytes) FROM files f WHERE f.owner_user_id = $1), 0)::bigint AS used_bytes
FROM users u WHERE u.id = $1;

-- name: CountPublished :one
-- 用户已发布到皮肤库的条目数。
SELECT COUNT(*) FROM skinlib_items WHERE owner_user_id = $1;

-- name: RecordTexture :one
-- 登记纹理字节数与归属（key 冲突时忽略——sha256 去重）；返回 key 表示本次新增。
INSERT INTO files (key, owner_user_id, size_bytes) VALUES ($1, $2, $3)
ON CONFLICT (key) DO NOTHING RETURNING key;

-- name: UnrecordTexture :exec
-- 撤销纹理登记（配额拒绝时回滚本次新增）。
DELETE FROM files WHERE key = $1;
