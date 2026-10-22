// Package pgstore 提供 Postgres 实现的 yggdrasil.Store（pgx，手写查询）。
// 说明：sqlc codegen 待 DB 工具链就绪后引入（见 ADR-0001）；当前用 pgx 手写查询。
package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 是 Postgres 实现的 yggdrasil.Store。
type Store struct {
	pool *pgxpool.Pool
}

// New 连接并 ping 数据库。
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close 关闭连接池。
func (s *Store) Close() { s.pool.Close() }

// Authenticate 校验凭证（Yggdrasil 的 username 即邮箱）。
func (s *Store) Authenticate(ctx context.Context, username, password string) (string, error) {
	var id, hash string
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, username).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	ok, err := passwd.Verify(password, hash)
	if err != nil || !ok {
		return "", yggdrasil.ErrInvalidCredentials
	}
	return id, nil
}

func (s *Store) PlayerByUser(ctx context.Context, userID string) (*yggdrasil.Player, error) {
	players, err := s.PlayersByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(players) == 0 {
		return nil, yggdrasil.ErrNotFound
	}
	return &players[0], nil
}

func (s *Store) PlayersByUser(ctx context.Context, userID string) ([]yggdrasil.Player, error) {
	rows, err := s.pool.Query(ctx, `SELECT uuid, name FROM players WHERE owner_user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]yggdrasil.Player, 0)
	for rows.Next() {
		var p yggdrasil.Player
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AddPlayer(ctx context.Context, userID, name string) (*yggdrasil.Player, error) {
	playerUUID := mc.UUIDFromName(name)
	_, err := s.pool.Exec(ctx, `INSERT INTO players (uuid, name, owner_user_id) VALUES ($1, $2, $3)`,
		playerUUID, name, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	return &yggdrasil.Player{ID: playerUUID, Name: name}, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, clientToken string) (string, error) {
	p, err := s.PlayerByUser(ctx, userID)
	if err != nil {
		return "", err
	}
	accessToken := randomToken()
	refreshToken := randomToken()
	_, err = s.pool.Exec(ctx, `INSERT INTO sessions (user_id, player_id, access_token, client_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		userID, p.ID, accessToken, clientToken, refreshToken, time.Now().Add(24*time.Hour))
	if err != nil {
		return "", err
	}
	return accessToken, nil
}

func (s *Store) Validate(ctx context.Context, accessToken, clientToken string) (*yggdrasil.Session, error) {
	var sess yggdrasil.Session
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT user_id, player_id, client_token, expires_at FROM sessions WHERE access_token = $1`, accessToken).
		Scan(&sess.UserID, &sess.PlayerID, &sess.ClientToken, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(expiresAt) {
		return nil, yggdrasil.ErrInvalidToken
	}
	if clientToken != "" && sess.ClientToken != clientToken {
		return nil, yggdrasil.ErrInvalidToken
	}
	sess.AccessToken = accessToken
	return &sess, nil
}

func (s *Store) Refresh(ctx context.Context, accessToken, clientToken string) (string, error) {
	sess, err := s.Validate(ctx, accessToken, clientToken)
	if err != nil {
		return "", err
	}
	newToken := randomToken()
	newRefresh := randomToken()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE access_token = $1`, accessToken); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (user_id, player_id, access_token, client_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		sess.UserID, sess.PlayerID, newToken, sess.ClientToken, newRefresh, time.Now().Add(24*time.Hour)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return newToken, nil
}

func (s *Store) Invalidate(ctx context.Context, accessToken, clientToken string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE access_token = $1 AND ($2 = '' OR client_token = $2)`, accessToken, clientToken)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return yggdrasil.ErrInvalidToken
	}
	return nil
}

func (s *Store) Signout(ctx context.Context, username, password string) error {
	_, err := s.Authenticate(ctx, username, password)
	return err
}

func (s *Store) BindServer(ctx context.Context, accessToken, selectedProfile, serverID string) error {
	sess, err := s.Validate(ctx, accessToken, "")
	if err != nil {
		return err
	}
	if sess.PlayerID != selectedProfile {
		return yggdrasil.ErrInvalidToken
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO server_binds (server_id, player_id) VALUES ($1, $2)
		ON CONFLICT (server_id) DO UPDATE SET player_id = EXCLUDED.player_id, created_at = now()`, serverID, sess.PlayerID)
	return err
}

func (s *Store) HasJoined(ctx context.Context, username, serverID string) (*yggdrasil.ProfileData, error) {
	pd := &yggdrasil.ProfileData{}
	err := s.pool.QueryRow(ctx, `SELECT p.uuid, p.name, p.model, p.skin_key, p.cape_key, p.premium
		FROM server_binds b JOIN players p ON p.uuid = b.player_id
		WHERE b.server_id = $1 AND p.name = $2`, serverID, username).
		Scan(&pd.Player.ID, &pd.Player.Name, &pd.Model, &pd.SkinKey, &pd.CapeKey, &pd.Premium)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pd, nil
}

func (s *Store) ProfileByUUID(ctx context.Context, uuid string) (*yggdrasil.ProfileData, error) {
	pd := &yggdrasil.ProfileData{}
	err := s.pool.QueryRow(ctx, `SELECT uuid, name, model, skin_key, cape_key, premium FROM players WHERE uuid = $1`, uuid).
		Scan(&pd.Player.ID, &pd.Player.Name, &pd.Model, &pd.SkinKey, &pd.CapeKey, &pd.Premium)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pd, nil
}

func (s *Store) ProfileByName(ctx context.Context, name string) (*yggdrasil.ProfileData, error) {
	pd := &yggdrasil.ProfileData{}
	err := s.pool.QueryRow(ctx, `SELECT uuid, name, model, skin_key, cape_key, premium FROM players WHERE name = $1`, name).
		Scan(&pd.Player.ID, &pd.Player.Name, &pd.Model, &pd.SkinKey, &pd.CapeKey, &pd.Premium)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pd, nil
}

func (s *Store) LookupNames(ctx context.Context, names []string) ([]yggdrasil.Player, error) {
	rows, err := s.pool.Query(ctx, `SELECT uuid, name FROM players WHERE name = ANY($1)`, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]yggdrasil.Player, 0)
	for rows.Next() {
		var p yggdrasil.Player
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetSkin(ctx context.Context, uuid, key, model string) error {
	_, err := s.pool.Exec(ctx, `UPDATE players SET skin_key = $2, model = $3 WHERE uuid = $1`, uuid, key, model)
	return err
}

func (s *Store) SetCape(ctx context.Context, uuid, key string) error {
	_, err := s.pool.Exec(ctx, `UPDATE players SET cape_key = $2 WHERE uuid = $1`, uuid, key)
	return err
}

func (s *Store) SetPremium(ctx context.Context, uuid string, premium bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE players SET premium = $2 WHERE uuid = $1`, uuid, premium)
	return err
}

func (s *Store) CreateAccount(ctx context.Context, email, passwordHash, nickname, playerName string) (*yggdrasil.Account, error) {
	playerUUID := mc.UUIDFromName(playerName)
	var id string
	err := s.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, nickname) VALUES ($1, $2, $3) RETURNING id`,
		email, passwordHash, nickname).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO players (uuid, name, owner_user_id) VALUES ($1, $2, $3)`,
		playerUUID, playerName, id); err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	return &yggdrasil.Account{ID: id, Email: email, Nickname: nickname, Verified: false}, nil
}

func (s *Store) AccountByEmail(ctx context.Context, email string) (*yggdrasil.Account, error) {
	acct := &yggdrasil.Account{Email: email}
	err := s.pool.QueryRow(ctx, `SELECT id, nickname, email_verified FROM users WHERE email = $1`, email).
		Scan(&acct.ID, &acct.Nickname, &acct.Verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return acct, nil
}

func (s *Store) CreateToken(ctx context.Context, userID, kind string, ttl time.Duration) (string, error) {
	token := randomToken()
	_, err := s.pool.Exec(ctx, `INSERT INTO verification_tokens (token, user_id, kind, expires_at) VALUES ($1, $2, $3, $4)`,
		token, userID, kind, time.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) ConsumeToken(ctx context.Context, kind, token string) (string, error) {
	var userID string
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT user_id, expires_at FROM verification_tokens WHERE token = $1 AND kind = $2`, token, kind).
		Scan(&userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	if time.Now().After(expiresAt) {
		return "", yggdrasil.ErrInvalidToken
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM verification_tokens WHERE token = $1`, token); err != nil {
		return "", err
	}
	return userID, nil
}

func (s *Store) MarkVerified(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET email_verified = true WHERE id = $1`, userID)
	return err
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, passwordHash)
	return err
}

func (s *Store) CreateInviteCode(ctx context.Context, code string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO invitation_codes (code) VALUES ($1)`, code)
	if err != nil {
		if isUniqueViolation(err) {
			return yggdrasil.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *Store) ConsumeInviteCode(ctx context.Context, code string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM invitation_codes WHERE code = $1`, code)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return yggdrasil.ErrInvalidToken
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
