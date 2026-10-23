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
	// ErrQuotaExceeded 表示超出存储或发布数量配额。
	ErrQuotaExceeded = errors.New("quota exceeded")
)

// Quota 是用户上传配额配置。StorageBytes<=0 表示不限存储；
// Published<=0 表示不限发布数量；Tiers 按用户 storage_tier 覆盖默认存储上限。
type Quota struct {
	StorageBytes int64
	Tiers        map[string]int64
	Published    int64
}

// LimitFor 返回指定 tier 的存储上限（字节）。
func (q Quota) LimitFor(tier string) int64 {
	if l, ok := q.Tiers[tier]; ok {
		return l
	}
	return q.StorageBytes
}

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
	UserID      string // 所属账号（配额检查用）
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
	Status      string `json:"status"` // listed | under_review | unlisted
	CreatedAt   string `json:"created_at"`
}

// Report 是一条内容举报。
type Report struct {
	ID             string `json:"id"`
	ReporterUserID string `json:"reporter_user_id"`
	ReporterName   string `json:"reporter_name"`
	TargetType     string `json:"target_type"` // skin | comment
	TargetID       string `json:"target_id"`
	Reason         string `json:"reason"`
	Status         string `json:"status"` // pending | dismissed | acted
	ReviewedBy     string `json:"reviewed_by"`
	CreatedAt      string `json:"created_at"`
	ReviewedAt     string `json:"reviewed_at,omitempty"`
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

// AuditEntry 是一条管理操作审计记录。
type AuditEntry struct {
	ID        string `json:"id"`
	Actor     string `json:"actor"` // "token" 或 "user:<id>"
	ActorIP   string `json:"actor_ip"`
	Action    string `json:"action"` // 如 admin.user.update
	Target    string `json:"target"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
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
	PublishSkin(ctx context.Context, ownerUserID, playerName, textureKey, description, model, status string) (*SkinLibItem, error)
	ListSkinLib(ctx context.Context, query, sort string, offset, limit int) ([]SkinLibItem, int64, error)
	GetSkinLibItem(ctx context.Context, itemID string) (*SkinLibItem, error)
	LikeSkin(ctx context.Context, itemID string) error
	CommentSkin(ctx context.Context, itemID, userID, content string) error
	ListSkinComments(ctx context.Context, itemID string) ([]SkinComment, error)
	DeleteSkinComment(ctx context.Context, commentID string) error

	// 举报与审核
	// ReportTarget 举报皮肤/评论；同一用户对同一目标重复举报返回 ErrAlreadyExists。
	ReportTarget(ctx context.Context, reporterUserID, targetType, targetID, reason string) error
	// ListReports 按状态列出举报（status 空=全部），含总数。
	ListReports(ctx context.Context, status string, limit, offset int) ([]Report, int64, error)
	// ResolveReport 标记举报处理结果（dismissed|acted）并记录处理人。
	ResolveReport(ctx context.Context, reportID, reviewerUserID, status string) error
	// SetSkinStatus 设置皮肤条目状态（listed|under_review|unlisted）。
	SetSkinStatus(ctx context.Context, itemID, status string) error
	// AdminListSkins 按状态列出皮肤条目（status 空=全部），含总数。
	AdminListSkins(ctx context.Context, status string, limit, offset int) ([]SkinLibItem, int64, error)

	// 配额
	// UserStorage 返回用户的存储 tier 与已用字节（按上传者归属的纹理累计，sha256 去重）。
	UserStorage(ctx context.Context, userID string) (tier string, usedBytes int64, err error)
	// CountPublished 返回用户已发布到皮肤库的条目数。
	CountPublished(ctx context.Context, userID string) (int64, error)
	// RecordTexture 登记纹理字节数与归属；added=false 表示 key 已存在（去重命中）。
	RecordTexture(ctx context.Context, key, ownerUserID string, size int64) (added bool, err error)
	// UnrecordTexture 撤销纹理登记（配额拒绝时回滚本次新增）。
	UnrecordTexture(ctx context.Context, key string) error

	// 管理后台
	ListUsers(ctx context.Context, query string, limit, offset int) ([]AdminUser, error)
	CountUsers(ctx context.Context, query string) (int64, error)
	UpdateUser(ctx context.Context, userID, role, status string) error
	UserRole(ctx context.Context, userID string) (string, error)
	DeleteSkinLibItem(ctx context.Context, itemID string) error
	WriteAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, limit, offset int) ([]AuditEntry, int64, error)
	SetSetting(ctx context.Context, key string, value []byte) error
	GetSetting(ctx context.Context, key string) ([]byte, error)
}
