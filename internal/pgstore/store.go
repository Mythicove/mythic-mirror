// Package pgstore 提供 Postgres 实现的 yggdrasil.Store（sqlc 生成的类型安全查询）。
package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	db "github.com/Mythicove/mythic-mirror/internal/pgstore/db"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 是 Postgres 实现的 yggdrasil.Store。
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
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
	return &Store{pool: pool, q: db.New(pool)}, nil
}

// Close 关闭连接池。
func (s *Store) Close() { s.pool.Close() }

func (s *Store) Authenticate(ctx context.Context, username, password string) (string, error) {
	row, err := s.q.GetUserAuth(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	ok, err := passwd.Verify(password, row.PasswordHash)
	if err != nil || !ok {
		return "", yggdrasil.ErrInvalidCredentials
	}
	return row.ID, nil
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
	rows, err := s.q.GetPlayersByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.Player, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.Player{ID: r.Uuid, Name: r.Name})
	}
	return out, nil
}

func (s *Store) AddPlayer(ctx context.Context, userID, name string) (*yggdrasil.Player, error) {
	row, err := s.q.CreatePlayer(ctx, db.CreatePlayerParams{Uuid: mc.UUIDFromName(name), Name: name, OwnerUserID: userID})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	return &yggdrasil.Player{ID: row.Uuid, Name: row.Name}, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, clientToken string) (string, error) {
	p, err := s.PlayerByUser(ctx, userID)
	if err != nil {
		return "", err
	}
	accessToken := randomToken()
	refreshToken := randomToken()
	err = s.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:       userID,
		PlayerID:     p.ID,
		AccessToken:  accessToken,
		ClientToken:  clientToken,
		RefreshToken: refreshToken,
		ExpiresAt:    tstz(time.Now().Add(24 * time.Hour)),
	})
	if err != nil {
		return "", err
	}
	return accessToken, nil
}

func (s *Store) Validate(ctx context.Context, accessToken, clientToken string) (*yggdrasil.Session, error) {
	row, err := s.q.GetSession(ctx, accessToken)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(row.ExpiresAt.Time) {
		return nil, yggdrasil.ErrInvalidToken
	}
	if clientToken != "" && row.ClientToken != clientToken {
		return nil, yggdrasil.ErrInvalidToken
	}
	return &yggdrasil.Session{UserID: row.UserID, PlayerID: row.PlayerID, AccessToken: accessToken, ClientToken: row.ClientToken}, nil
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
	q := s.q.WithTx(tx)
	if _, err := q.DeleteSessionByToken(ctx, accessToken); err != nil {
		return "", err
	}
	if err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID: sess.UserID, PlayerID: sess.PlayerID, AccessToken: newToken, ClientToken: sess.ClientToken,
		RefreshToken: newRefresh, ExpiresAt: tstz(time.Now().Add(24 * time.Hour)),
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return newToken, nil
}

func (s *Store) Invalidate(ctx context.Context, accessToken, clientToken string) error {
	var n int64
	var err error
	if clientToken == "" {
		n, err = s.q.DeleteSessionByToken(ctx, accessToken)
	} else {
		n, err = s.q.DeleteSessionByTokenAndClient(ctx, db.DeleteSessionByTokenAndClientParams{AccessToken: accessToken, ClientToken: clientToken})
	}
	if err != nil {
		return err
	}
	if n == 0 {
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
	return s.q.BindServer(ctx, db.BindServerParams{ServerID: serverID, PlayerID: sess.PlayerID})
}

func (s *Store) HasJoined(ctx context.Context, username, serverID string) (*yggdrasil.ProfileData, error) {
	row, err := s.q.HasJoined(ctx, db.HasJoinedParams{ServerID: serverID, Name: username})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid}, nil
}

func (s *Store) ProfileByUUID(ctx context.Context, uuid string) (*yggdrasil.ProfileData, error) {
	row, err := s.q.GetProfileByUUID(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid}, nil
}

func (s *Store) ProfileByName(ctx context.Context, name string) (*yggdrasil.ProfileData, error) {
	row, err := s.q.GetProfileByName(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid}, nil
}

func (s *Store) LookupNames(ctx context.Context, names []string) ([]yggdrasil.Player, error) {
	rows, err := s.q.LookupNames(ctx, names)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.Player, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.Player{ID: r.Uuid, Name: r.Name})
	}
	return out, nil
}

func (s *Store) SetSkin(ctx context.Context, uuid, key, model string) error {
	return s.q.SetSkin(ctx, db.SetSkinParams{Uuid: uuid, SkinKey: key, Model: model})
}

func (s *Store) SetCape(ctx context.Context, uuid, key string) error {
	return s.q.SetCape(ctx, db.SetCapeParams{Uuid: uuid, CapeKey: key})
}

func (s *Store) SetPremium(ctx context.Context, uuid, premiumUUID string) error {
	return s.q.SetPremium(ctx, db.SetPremiumParams{Uuid: uuid, PremiumUuid: premiumUUID})
}

func (s *Store) CreateAccount(ctx context.Context, email, passwordHash, nickname, playerName string) (*yggdrasil.Account, error) {
	id, err := s.q.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: passwordHash, Nickname: nickname})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	if _, err := s.q.CreatePlayer(ctx, db.CreatePlayerParams{Uuid: mc.UUIDFromName(playerName), Name: playerName, OwnerUserID: id}); err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	return &yggdrasil.Account{ID: id, Email: email, Nickname: nickname, Verified: false}, nil
}

func (s *Store) AccountByEmail(ctx context.Context, email string) (*yggdrasil.Account, error) {
	row, err := s.q.GetAccountByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.Account{ID: row.ID, Email: email, Nickname: row.Nickname, Verified: row.EmailVerified}, nil
}

func (s *Store) AccountByID(ctx context.Context, userID string) (*yggdrasil.Account, error) {
	row, err := s.q.GetAccountByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.Account{ID: row.ID, Email: row.Email, Nickname: row.Nickname, Verified: row.EmailVerified}, nil
}

func (s *Store) CreateToken(ctx context.Context, userID, kind string, ttl time.Duration) (string, error) {
	token := randomToken()
	err := s.q.CreateVerificationToken(ctx, db.CreateVerificationTokenParams{Token: token, UserID: userID, Kind: kind, ExpiresAt: tstz(time.Now().Add(ttl))})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) ConsumeToken(ctx context.Context, kind, token string) (string, error) {
	row, err := s.q.GetVerificationToken(ctx, db.GetVerificationTokenParams{Token: token, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	if time.Now().After(row.ExpiresAt.Time) {
		return "", yggdrasil.ErrInvalidToken
	}
	if err := s.q.DeleteVerificationToken(ctx, token); err != nil {
		return "", err
	}
	return row.UserID, nil
}

func (s *Store) MarkVerified(ctx context.Context, userID string) error {
	return s.q.MarkVerified(ctx, userID)
}

func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	return s.q.UpdatePassword(ctx, db.UpdatePasswordParams{ID: userID, PasswordHash: passwordHash})
}

func (s *Store) CreateInviteCode(ctx context.Context, code string) error {
	return s.q.CreateInviteCode(ctx, code)
}

func (s *Store) ConsumeInviteCode(ctx context.Context, code string) error {
	n, err := s.q.DeleteInviteCode(ctx, code)
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrInvalidToken
	}
	return nil
}

func (s *Store) PublishSkin(ctx context.Context, ownerUserID, playerName, textureKey, description, model string) (*yggdrasil.SkinLibItem, error) {
	row, err := s.q.CreateSkinLibItem(ctx, db.CreateSkinLibItemParams{OwnerUserID: ownerUserID, PlayerName: playerName, TextureKey: textureKey, Description: description, Model: model})
	if err != nil {
		return nil, err
	}
	return &yggdrasil.SkinLibItem{ID: row.ID, OwnerUserID: row.OwnerUserID, PlayerName: row.PlayerName, TextureKey: row.TextureKey, Description: row.Description, Model: row.Model, Likes: int(row.Likes)}, nil
}

func (s *Store) ListSkinLib(ctx context.Context, offset, limit int) ([]yggdrasil.SkinLibItem, error) {
	rows, err := s.q.ListSkinLib(ctx, db.ListSkinLibParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.SkinLibItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.SkinLibItem{ID: r.ID, OwnerUserID: r.OwnerUserID, PlayerName: r.PlayerName, TextureKey: r.TextureKey, Description: r.Description, Model: r.Model, Likes: int(r.Likes)})
	}
	return out, nil
}

func (s *Store) LikeSkin(ctx context.Context, itemID string) error {
	return s.q.LikeSkin(ctx, itemID)
}

func (s *Store) CommentSkin(ctx context.Context, itemID, userID, content string) error {
	return s.q.CreateSkinLibComment(ctx, db.CreateSkinLibCommentParams{ItemID: itemID, UserID: userID, Content: content})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// ListUsers 返回管理后台用户列表（含玩家名）。
func (s *Store) ListUsers(ctx context.Context, query string, limit, offset int) ([]yggdrasil.AdminUser, error) {
	rows, err := s.q.ListUsers(ctx, db.ListUsersParams{Column1: query, Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.AdminUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.AdminUser{
			ID:          r.ID,
			Email:       r.Email,
			Nickname:    r.Nickname,
			Role:        r.Role,
			Status:      r.Status,
			Verified:    r.EmailVerified,
			CreatedAt:   r.CreatedAt.Time.Format(time.RFC3339),
			PlayerNames: splitNames(r.PlayerNames),
		})
	}
	return out, nil
}

// CountUsers 统计用户数。
func (s *Store) CountUsers(ctx context.Context, query string) (int64, error) {
	return s.q.CountUsers(ctx, query)
}

// UpdateUser 更新用户角色与状态。
func (s *Store) UpdateUser(ctx context.Context, userID, role, status string) error {
	return s.q.UpdateUserRoleStatus(ctx, db.UpdateUserRoleStatusParams{ID: userID, Role: role, Status: status})
}

// UserRole 返回用户角色。
func (s *Store) UserRole(ctx context.Context, userID string) (string, error) {
	role, err := s.q.UserRole(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrNotFound
	}
	return role, err
}

// DeleteSkinLibItem 删除皮肤库条目。
func (s *Store) DeleteSkinLibItem(ctx context.Context, itemID string) error {
	n, err := s.q.DeleteSkinLibItem(ctx, itemID)
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

// SetSetting 写入键值配置。
func (s *Store) SetSetting(ctx context.Context, key string, value []byte) error {
	return s.q.SetSetting(ctx, db.SetSettingParams{Key: key, Value: value})
}

// GetSetting 读取键值配置。
func (s *Store) GetSetting(ctx context.Context, key string) ([]byte, error) {
	v, err := s.q.GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	return v, err
}

func splitNames(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
