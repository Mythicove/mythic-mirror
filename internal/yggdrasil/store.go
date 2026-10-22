package yggdrasil

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrInvalidCredentials 表示凭证错误。
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken 表示 token 无效。
	ErrInvalidToken = errors.New("invalid token")
	// ErrNotFound 表示资源不存在。
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists 表示资源已存在（邮箱/玩家名冲突）。
	ErrAlreadyExists = errors.New("already exists")
)

const (
	// TokenVerify 是邮箱验证 token 类型。
	TokenVerify = "verify"
	// TokenReset 是密码重置 token 类型。
	TokenReset = "reset"
)

// Session 是 Yggdrasil 会话。
type Session struct {
	UserID      string
	PlayerID    string
	AccessToken string
	ClientToken string
}

// ProfileData 是带纹理键的玩家资料（纹理内容存对象存储，此处只存键）。
type ProfileData struct {
	Player  Player
	SkinKey string
	CapeKey string
	Model   string // default | slim
	Premium bool   // 正版账号
}

// Account 是账号信息。
type Account struct {
	ID       string
	Email    string
	Nickname string
	Verified bool
}

// SkinLibItem 是皮肤库条目。
type SkinLibItem struct {
	ID          string
	OwnerUserID string
	PlayerName  string
	TextureKey  string
	Description string
	Model       string
	Likes       int
}

// Store 是 Yggdrasil 与账号体系所需的存储抽象。
// M3 时 Postgres 实现可同时满足 shared/auth.Auth（Web/API 层）与本接口（Yggdrasil 层）。
type Store interface {
	Authenticate(ctx context.Context, username, password string) (userID string, err error)
	PlayerByUser(ctx context.Context, userID string) (*Player, error)
	PlayersByUser(ctx context.Context, userID string) ([]Player, error)
	AddPlayer(ctx context.Context, userID, name string) (*Player, error)
	CreateSession(ctx context.Context, userID, clientToken string) (accessToken string, err error)
	Validate(ctx context.Context, accessToken, clientToken string) (*Session, error)
	Refresh(ctx context.Context, accessToken, clientToken string) (newAccessToken string, err error)
	Invalidate(ctx context.Context, accessToken, clientToken string) error
	Signout(ctx context.Context, username, password string) error
	BindServer(ctx context.Context, accessToken, selectedProfile, serverID string) error
	HasJoined(ctx context.Context, username, serverID string) (*ProfileData, error)
	ProfileByUUID(ctx context.Context, uuid string) (*ProfileData, error)
	ProfileByName(ctx context.Context, name string) (*ProfileData, error)
	LookupNames(ctx context.Context, names []string) ([]Player, error)
	SetSkin(ctx context.Context, uuid, key, model string) error
	SetCape(ctx context.Context, uuid, key string) error
	SetPremium(ctx context.Context, uuid string, premium bool) error
	CreateAccount(ctx context.Context, email, passwordHash, nickname, playerName string) (*Account, error)
	AccountByEmail(ctx context.Context, email string) (*Account, error)
	CreateToken(ctx context.Context, userID, kind string, ttl time.Duration) (token string, err error)
	ConsumeToken(ctx context.Context, kind, token string) (userID string, err error)
	MarkVerified(ctx context.Context, userID string) error
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	CreateInviteCode(ctx context.Context, code string) error
	ConsumeInviteCode(ctx context.Context, code string) error
	PublishSkin(ctx context.Context, ownerUserID, playerName, textureKey, description, model string) (*SkinLibItem, error)
	ListSkinLib(ctx context.Context, offset, limit int) ([]SkinLibItem, error)
	LikeSkin(ctx context.Context, itemID string) error
	CommentSkin(ctx context.Context, itemID, userID, content string) error
}
