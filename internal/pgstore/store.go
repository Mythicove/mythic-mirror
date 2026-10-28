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

// AuthenticatePlayer 是启动器登录：按玩家名定位玩家，再校验该玩家的独立密码。
// 主密码在这里完全不参与，管理员因此可以关掉主密码登录而启动器照常工作。
// 校验按最近使用倒序，常用凭证先命中；Argon2id 每次校验都要吃满内存带宽，
// 故凭证条数有上限（account.MaxCredentialsPerPlayer），失败尝试的成本才是可控的。
func (s *Store) AuthenticatePlayer(ctx context.Context, playerName, password string) (*yggdrasil.LauncherAuth, error) {
	row, err := s.q.GetPlayerForLauncher(ctx, playerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if row.UserStatus != "active" {
		return nil, yggdrasil.ErrAccountDisabled
	}
	hashes, err := s.q.PlayerCredentialHashes(ctx, row.Uuid)
	if err != nil {
		return nil, err
	}
	for _, h := range hashes {
		ok, err := passwd.Verify(password, h.PasswordHash)
		if err != nil || !ok {
			continue
		}
		_ = s.q.TouchPlayerCredential(ctx, h.ID)
		return &yggdrasil.LauncherAuth{
			Player:       yggdrasil.Player{ID: row.Uuid, Name: row.Name, Premium: row.PremiumUuid != ""},
			UserID:       row.OwnerUserID,
			CredentialID: h.ID,
		}, nil
	}
	return nil, yggdrasil.ErrInvalidCredentials
}

func (s *Store) CreatePlayerCredential(ctx context.Context, playerUUID, name, passwordHash string) (*yggdrasil.PlayerCredential, error) {
	row, err := s.q.CreatePlayerCredential(ctx, db.CreatePlayerCredentialParams{
		PlayerUuid: playerUUID, Name: name, PasswordHash: passwordHash,
	})
	if err != nil {
		return nil, err
	}
	return credential(row.ID, row.Name, row.CreatedAt, row.LastUsedAt), nil
}

func (s *Store) ListPlayerCredentials(ctx context.Context, playerUUID string) ([]yggdrasil.PlayerCredential, error) {
	rows, err := s.q.ListPlayerCredentials(ctx, playerUUID)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.PlayerCredential, 0, len(rows))
	for _, r := range rows {
		out = append(out, *credential(r.ID, r.Name, r.CreatedAt, r.LastUsedAt))
	}
	return out, nil
}

// DeletePlayerCredential 撤销独立密码；由它建立的会话经外键级联一并销毁。
func (s *Store) DeletePlayerCredential(ctx context.Context, playerUUID, credentialID string) error {
	n, err := s.q.DeletePlayerCredential(ctx, db.DeletePlayerCredentialParams{ID: credentialID, PlayerUuid: playerUUID})
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

func (s *Store) UpsertIdentity(ctx context.Context, provider, subject, userID, email string) error {
	return s.q.UpsertIdentity(ctx, db.UpsertIdentityParams{
		Provider: provider, Subject: subject, UserID: userID, Email: email,
	})
}

func (s *Store) IdentityUser(ctx context.Context, provider, subject string) (string, error) {
	userID, err := s.q.GetIdentity(ctx, db.GetIdentityParams{Provider: provider, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

func (s *Store) Identities(ctx context.Context, userID string) ([]yggdrasil.Identity, error) {
	rows, err := s.q.ListIdentitiesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.Identity, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.Identity{
			Provider: r.Provider, Subject: r.Subject, Email: r.Email,
			CreatedAt: r.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (s *Store) CountPasswordOnlyUsers(ctx context.Context) (int64, error) {
	return s.q.CountPasswordOnlyUsers(ctx)
}

func credential(id, name string, createdAt, lastUsedAt pgtype.Timestamptz) *yggdrasil.PlayerCredential {
	c := &yggdrasil.PlayerCredential{ID: id, Name: name, CreatedAt: createdAt.Time.Format(time.RFC3339)}
	if lastUsedAt.Valid {
		c.LastUsedAt = lastUsedAt.Time.Format(time.RFC3339)
	}
	return c
}

func (s *Store) PlayersByUser(ctx context.Context, userID string) ([]yggdrasil.Player, error) {
	rows, err := s.q.GetPlayersByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.Player, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.Player{ID: r.Uuid, Name: r.Name, Premium: r.PremiumUuid != ""})
	}
	return out, nil
}

// AddPlayer 新建玩家；limit<=0 表示不限（管理员策略决定，见 yggdrasil.AuthPolicy.MaxPlayers）。
func (s *Store) AddPlayer(ctx context.Context, userID, name string, limit int) (*yggdrasil.Player, error) {
	if limit > 0 {
		count, err := s.q.CountPlayersByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		if count >= int64(limit) {
			return nil, yggdrasil.ErrPlayerLimit
		}
	}
	row, err := s.q.CreatePlayer(ctx, db.CreatePlayerParams{Uuid: mc.UUIDFromName(name), Name: name, OwnerUserID: userID})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	return &yggdrasil.Player{ID: row.Uuid, Name: row.Name}, nil
}

// RenamePlayer 改名：uuid 由名字派生，故必须同时换主键，并让旧 uuid 的会话/服务器绑定失效
// （启动器需重新登录）。已绑定正版的玩家名字来自 Mojang，拒绝本地改名以免与正版档案脱节。
func (s *Store) RenamePlayer(ctx context.Context, userID, oldName, newName string) (*yggdrasil.Player, error) {
	prof, err := s.q.GetProfileByName(ctx, oldName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if prof.OwnerUserID != userID {
		return nil, yggdrasil.ErrNotFound
	}
	if prof.PremiumUuid != "" {
		return nil, yggdrasil.ErrPremiumBound
	}
	newUUID := mc.UUIDFromName(newName)
	if _, err := s.q.GetProfileByName(ctx, newName); err == nil {
		return nil, yggdrasil.ErrAlreadyExists
	}
	if _, err := s.q.GetProfileByUUID(ctx, newUUID); err == nil {
		return nil, yggdrasil.ErrAlreadyExists
	}
	n, err := s.q.RenamePlayer(ctx, db.RenamePlayerParams{
		Uuid: prof.Uuid, OwnerUserID: userID,
		Uuid_2: newUUID, Name: newName,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yggdrasil.ErrAlreadyExists
		}
		return nil, err
	}
	if n == 0 {
		return nil, yggdrasil.ErrNotFound
	}
	// 旧 uuid 的会话与绑定已无对应档案，先清掉（清理失败不应让整个改名失败）。
	// 独立密码经 player_credentials.player_uuid 的 ON UPDATE CASCADE 跟随新 uuid。
	_ = s.q.DeleteSessionsByPlayer(ctx, prof.Uuid)
	_ = s.q.DeleteServerBindsByPlayer(ctx, prof.Uuid)
	return &yggdrasil.Player{ID: newUUID, Name: newName}, nil
}

// DeletePlayer 删除玩家及其会话/服务器绑定；拒绝删除账号下最后一个玩家（否则无法进服）。
func (s *Store) DeletePlayer(ctx context.Context, userID, name string) error {
	prof, err := s.q.GetProfileByName(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return yggdrasil.ErrNotFound
	}
	if err != nil {
		return err
	}
	if prof.OwnerUserID != userID {
		return yggdrasil.ErrNotFound
	}
	count, err := s.q.CountPlayersByUser(ctx, userID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return yggdrasil.ErrLastPlayer
	}
	_ = s.q.DeleteSessionsByPlayer(ctx, prof.Uuid)
	_ = s.q.DeleteServerBindsByPlayer(ctx, prof.Uuid)
	n, err := s.q.DeletePlayer(ctx, db.DeletePlayerParams{Uuid: prof.Uuid, OwnerUserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

// CreateSession 建会话。playerID 空串 + credentialID 空 = 网站/OIDC 会话（不代表任何玩家）；
// 启动器会话两者都非空，credentialID 让「撤销独立密码」能连带销毁它建立的会话。
func (s *Store) CreateSession(ctx context.Context, userID, playerID, credentialID, clientToken string) (string, error) {
	accessToken := randomToken()
	refreshToken := randomToken()
	err := s.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:       userID,
		PlayerID:     playerID,
		AccessToken:  accessToken,
		ClientToken:  clientToken,
		RefreshToken: refreshToken,
		ExpiresAt:    tstz(time.Now().Add(24 * time.Hour)),
		CredentialID: nullableText(credentialID),
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
	if row.UserStatus != "active" {
		return nil, yggdrasil.ErrAccountDisabled
	}
	return &yggdrasil.Session{
		UserID: row.UserID, PlayerID: row.PlayerID, AccessToken: accessToken,
		ClientToken: row.ClientToken, CredentialID: row.CredentialID.String,
	}, nil
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
		CredentialID: nullableText(sess.CredentialID),
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

func (s *Store) Signout(ctx context.Context, playerName, password string) error {
	auth, err := s.AuthenticatePlayer(ctx, playerName, password)
	if err != nil {
		return err
	}
	return s.q.DeleteSessionsByPlayer(ctx, auth.Player.ID)
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
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid, UserID: row.OwnerUserID}, nil
}

func (s *Store) ProfileByUUID(ctx context.Context, uuid string) (*yggdrasil.ProfileData, error) {
	row, err := s.q.GetProfileByUUID(ctx, uuid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid, UserID: row.OwnerUserID}, nil
}

func (s *Store) ProfileByName(ctx context.Context, name string) (*yggdrasil.ProfileData, error) {
	row, err := s.q.GetProfileByName(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &yggdrasil.ProfileData{Player: yggdrasil.Player{ID: row.Uuid, Name: row.Name}, SkinKey: row.SkinKey, CapeKey: row.CapeKey, Model: row.Model, PremiumUUID: row.PremiumUuid, UserID: row.OwnerUserID}, nil
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
	userID, err := s.q.ConsumeVerificationToken(ctx, db.ConsumeVerificationTokenParams{Token: token, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", yggdrasil.ErrInvalidToken
	}
	if err != nil {
		return "", err
	}
	return userID, nil
}

// DeleteUserSessions 销毁某用户的全部会话（封禁/停用时调用）。
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	return s.q.DeleteSessionsByUser(ctx, userID)
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

func (s *Store) PublishSkin(ctx context.Context, ownerUserID, playerName, textureKey, description, model, status string) (*yggdrasil.SkinLibItem, error) {
	if status == "" {
		status = "listed"
	}
	row, err := s.q.CreateSkinLibItem(ctx, db.CreateSkinLibItemParams{OwnerUserID: ownerUserID, PlayerName: playerName, TextureKey: textureKey, Description: description, Model: model, Status: status})
	if err != nil {
		return nil, err
	}
	return skinLibItemFrom(row.ID, row.OwnerUserID, row.PlayerName, row.TextureKey, row.Description, row.Model, row.Likes, row.Status, row.CreatedAt), nil
}

func skinLibItemFrom(id, owner, playerName, textureKey, description, model string, likes int32, status string, createdAt pgtype.Timestamptz) *yggdrasil.SkinLibItem {
	return &yggdrasil.SkinLibItem{
		ID: id, OwnerUserID: owner, PlayerName: playerName, TextureKey: textureKey,
		Description: description, Model: model, Likes: int(likes), Status: status, CreatedAt: createdAt.Time.Format(time.RFC3339),
	}
}

func (s *Store) ListSkinLib(ctx context.Context, query, sort string, offset, limit int) ([]yggdrasil.SkinLibItem, int64, error) {
	total, err := s.q.CountSkinLib(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.SearchSkinLib(ctx, db.SearchSkinLibParams{Column1: query, Column2: sort, Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, 0, err
	}
	out := make([]yggdrasil.SkinLibItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, *skinLibItemFrom(r.ID, r.OwnerUserID, r.PlayerName, r.TextureKey, r.Description, r.Model, r.Likes, r.Status, r.CreatedAt))
	}
	return out, total, nil
}

func (s *Store) GetSkinLibItem(ctx context.Context, itemID string) (*yggdrasil.SkinLibItem, error) {
	row, err := s.q.GetSkinLibItem(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, yggdrasil.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return skinLibItemFrom(row.ID, row.OwnerUserID, row.PlayerName, row.TextureKey, row.Description, row.Model, row.Likes, row.Status, row.CreatedAt), nil
}

// AdminListSkins 按状态列出皮肤条目（status 空=全部），最新在前。
func (s *Store) AdminListSkins(ctx context.Context, status string, limit, offset int) ([]yggdrasil.SkinLibItem, int64, error) {
	total, err := s.q.CountAdminSkinLib(ctx, status)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.AdminSearchSkinLib(ctx, db.AdminSearchSkinLibParams{Column1: status, Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, 0, err
	}
	out := make([]yggdrasil.SkinLibItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, *skinLibItemFrom(r.ID, r.OwnerUserID, r.PlayerName, r.TextureKey, r.Description, r.Model, r.Likes, r.Status, r.CreatedAt))
	}
	return out, total, nil
}

// SetSkinStatus 设置皮肤条目状态。
func (s *Store) SetSkinStatus(ctx context.Context, itemID, status string) error {
	n, err := s.q.SetSkinStatus(ctx, db.SetSkinStatusParams{ID: itemID, Status: status})
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

// ReportTarget 举报皮肤/评论；重复举报返回 ErrAlreadyExists。
func (s *Store) ReportTarget(ctx context.Context, reporterUserID, targetType, targetID, reason string) error {
	n, err := s.q.InsertReport(ctx, db.InsertReportParams{
		ReporterUserID: reporterUserID, TargetType: targetType, TargetID: targetID, Reason: reason,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrAlreadyExists
	}
	return nil
}

// ListReports 按状态列出举报（status 空=全部；pending 优先）。
func (s *Store) ListReports(ctx context.Context, status string, limit, offset int) ([]yggdrasil.Report, int64, error) {
	total, err := s.q.CountReports(ctx, status)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListReports(ctx, db.ListReportsParams{Column1: status, Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, 0, err
	}
	out := make([]yggdrasil.Report, 0, len(rows))
	for _, r := range rows {
		rep := yggdrasil.Report{
			ID: r.ID, ReporterUserID: r.ReporterUserID, ReporterName: r.ReporterName,
			TargetType: r.TargetType, TargetID: r.TargetID, Reason: r.Reason,
			Status: r.Status, ReviewedBy: r.ReviewedBy, CreatedAt: r.CreatedAt.Time.Format(time.RFC3339),
		}
		if r.ReviewedAt.Valid {
			rep.ReviewedAt = r.ReviewedAt.Time.Format(time.RFC3339)
		}
		out = append(out, rep)
	}
	return out, total, nil
}

// ResolveReport 标记举报处理结果。
func (s *Store) ResolveReport(ctx context.Context, reportID, reviewerUserID, status string) error {
	n, err := s.q.ResolveReport(ctx, db.ResolveReportParams{ID: reportID, Status: status, ReviewedBy: reviewerUserID})
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

func (s *Store) LikeSkin(ctx context.Context, itemID string) error {
	n, err := s.q.LikeSkin(ctx, itemID)
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

func (s *Store) CommentSkin(ctx context.Context, itemID, userID, content string) error {
	return s.q.CreateSkinLibComment(ctx, db.CreateSkinLibCommentParams{ItemID: itemID, UserID: userID, Content: content})
}

func (s *Store) ListSkinComments(ctx context.Context, itemID string) ([]yggdrasil.SkinComment, error) {
	rows, err := s.q.ListSkinLibComments(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]yggdrasil.SkinComment, 0, len(rows))
	for _, r := range rows {
		out = append(out, yggdrasil.SkinComment{
			ID: r.ID, ItemID: r.ItemID, UserID: r.UserID,
			AuthorName: r.AuthorName, Content: r.Content, CreatedAt: r.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	return out, nil
}

func (s *Store) DeleteSkinComment(ctx context.Context, commentID string) error {
	n, err := s.q.DeleteSkinLibComment(ctx, commentID)
	if err != nil {
		return err
	}
	if n == 0 {
		return yggdrasil.ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// nullableText 把空串映射为 SQL NULL（可空外键列用）。
func nullableText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
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
	// 空串语义为"不修改"：先取当前值补齐，避免把列覆盖成空。
	if role == "" {
		cur, err := s.UserRole(ctx, userID)
		if err != nil {
			return err
		}
		role = cur
	}
	if status == "" {
		cur, err := s.q.UserStatus(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return yggdrasil.ErrNotFound
		}
		if err != nil {
			return err
		}
		status = cur
	}
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

// WriteAudit 写入审计记录。
func (s *Store) WriteAudit(ctx context.Context, e yggdrasil.AuditEntry) error {
	return s.q.WriteAudit(ctx, db.WriteAuditParams{
		Actor: e.Actor, ActorIp: e.ActorIP, Action: e.Action, Target: e.Target, Detail: e.Detail,
	})
}

// ListAudit 按时间倒序返回审计记录；q 非空时对 action/actor/target 做 ILIKE 过滤。
// 该查询带动态过滤条件，未走 sqlc 生成物（避免为单一管理端过滤再生成一套）。
func (s *Store) ListAudit(ctx context.Context, q string, limit, offset int) ([]yggdrasil.AuditEntry, int64, error) {
	const where = `($1 = '' OR action ILIKE '%'||$1||'%' OR actor ILIKE '%'||$1||'%' OR target ILIKE '%'||$1||'%')`
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE `+where, q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, actor, actor_ip, action, target, detail, created_at FROM audit_log WHERE `+where+
			` ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]yggdrasil.AuditEntry, 0, limit)
	for rows.Next() {
		var r db.AuditLog
		if err := rows.Scan(&r.ID, &r.Actor, &r.ActorIp, &r.Action, &r.Target, &r.Detail, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, yggdrasil.AuditEntry{
			ID: r.ID, Actor: r.Actor, ActorIP: r.ActorIp, Action: r.Action,
			Target: r.Target, Detail: r.Detail, CreatedAt: r.CreatedAt.Time.Format(time.RFC3339),
		})
	}
	return out, total, rows.Err()
}

// UserStorage 返回用户存储 tier 与已用字节（名下纹理去重聚合）。
func (s *Store) UserStorage(ctx context.Context, userID string) (string, int64, error) {
	row, err := s.q.UserStorage(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, yggdrasil.ErrNotFound
		}
		return "", 0, err
	}
	return row.Tier, row.UsedBytes, nil
}

// CountPublished 返回用户已发布到皮肤库的条目数。
func (s *Store) CountPublished(ctx context.Context, userID string) (int64, error) {
	return s.q.CountPublished(ctx, userID)
}

// RecordTexture 登记纹理字节数与归属；added=false 表示 key 已存在（去重命中）。
func (s *Store) RecordTexture(ctx context.Context, key, ownerUserID string, size int64) (bool, error) {
	_, err := s.q.RecordTexture(ctx, db.RecordTextureParams{Key: key, OwnerUserID: ownerUserID, SizeBytes: size})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// UnrecordTexture 撤销纹理登记（配额拒绝时回滚）。
func (s *Store) UnrecordTexture(ctx context.Context, key string) error {
	return s.q.UnrecordTexture(ctx, key)
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
