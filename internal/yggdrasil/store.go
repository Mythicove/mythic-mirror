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
	Player      Player
	SkinKey     string
	CapeKey     string
	Model       string // default | slim
	PremiumUUID string // 正版玩家的真实 Mojang UUID（空=非正版）
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
	ID          string `json:"id"`
	OwnerUserID string `json:"owner_user_id"`
	PlayerName  string `json:"player_name"`
	TextureKey  string `json:"texture_key"`
	Description string `json:"description"`
	Model       string `json:"model"`
	Likes       int    `json:"likes"`
	CreatedAt   string `json:"created_at"`
}

// SkinComment 是皮肤库评论。
type SkinComment struct {
	ID         string `json:"id"`
	ItemID     string `json:"item_id"`
	UserID     string `json:"user_id"`
	AuthorName string `json:"author_name"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at"`
}

// AdminUser 是管理后台的用户视图。
type AdminUser struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Nickname    string   `json:"nickname"`
	Role        string   `json:"role"`
	Status      string   `json:"status"`
	Verified    bool     `json:"verified"`
	CreatedAt   string   `json:"created_at"`
	PlayerNames []string `json:"player_names"`
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
	SetPremium(ctx context.Context, uuid, premiumUUID string) error
	CreateAccount(ctx context.Context, email, passwordHash, nickname, playerName string) (*Account, error)
	AccountByEmail(ctx context.Context, email string) (*Account, error)
	AccountByID(ctx context.Context, userID string) (*Account, error)
	CreateToken(ctx context.Context, userID, kind string, ttl time.Duration) (token string, err error)
	ConsumeToken(ctx context.Context, kind, token string) (userID string, err error)
	MarkVerified(ctx context.Context, userID string) error
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	CreateInviteCode(ctx context.Context, code string) error
	ConsumeInviteCode(ctx context.Context, code string) error
	PublishSkin(ctx context.Context, ownerUserID, playerName, textureKey, description, model string) (*SkinLibItem, error)
	ListSkinLib(ctx context.Context, query, sort string, offset, limit int) ([]SkinLibItem, int64, error)
	GetSkinLibItem(ctx context.Context, itemID string) (*SkinLibItem, error)
	LikeSkin(ctx context.Context, itemID string) error
	CommentSkin(ctx context.Context, itemID, userID, content string) error
	ListSkinComments(ctx context.Context, itemID string) ([]SkinComment, error)
	DeleteSkinComment(ctx context.Context, commentID string) error

	// 管理后台
	ListUsers(ctx context.Context, query string, limit, offset int) ([]AdminUser, error)
	CountUsers(ctx context.Context, query string) (int64, error)
	UpdateUser(ctx context.Context, userID, role, status string) error
	UserRole(ctx context.Context, userID string) (string, error)
	DeleteSkinLibItem(ctx context.Context, itemID string) error
	SetSetting(ctx context.Context, key string, value []byte) error
	GetSetting(ctx context.Context, key string) ([]byte, error)
}
