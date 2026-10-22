-- name: GetUserAuth :one
SELECT id, password_hash FROM users WHERE email = $1;

-- name: GetPlayersByUser :many
SELECT uuid, name FROM players WHERE owner_user_id = $1 ORDER BY created_at;

-- name: CreatePlayer :one
INSERT INTO players (uuid, name, owner_user_id) VALUES ($1, $2, $3) RETURNING uuid, name;

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
SELECT p.uuid, p.name, p.model, p.skin_key, p.cape_key, p.premium_uuid
FROM server_binds b JOIN players p ON p.uuid = b.player_id
WHERE b.server_id = $1 AND p.name = $2;

-- name: GetProfileByUUID :one
SELECT uuid, name, model, skin_key, cape_key, premium_uuid FROM players WHERE uuid = $1;

-- name: GetProfileByName :one
SELECT uuid, name, model, skin_key, cape_key, premium_uuid FROM players WHERE name = $1;

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
INSERT INTO skinlib_items (owner_user_id, player_name, texture_key, description, model)
VALUES ($1, $2, $3, $4, $5) RETURNING id, owner_user_id, player_name, texture_key, description, model, likes;

-- name: ListSkinLib :many
SELECT id, owner_user_id, player_name, texture_key, description, model, likes
FROM skinlib_items ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: LikeSkin :exec
UPDATE skinlib_items SET likes = likes + 1 WHERE id = $1;

-- name: CreateSkinLibComment :exec
INSERT INTO skinlib_comments (item_id, user_id, content) VALUES ($1, $2, $3);
