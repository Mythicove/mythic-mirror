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
	// ErrPremiumBound 表示该玩家已绑定正版（Microsoft）：名字来自 Mojang，不可本地改名。
	ErrPremiumBound = errors.New("player is linked to a Microsoft account")
	// ErrLastPlayer 表示不能删除账号下最后一个玩家（否则无档案可进服）。
	ErrLastPlayer = errors.New("cannot delete the only player")
	// ErrPlayerLimit 表示已达到管理员设定的单账号玩家数上限。
	ErrPlayerLimit = errors.New("player limit reached")
	// ErrCredentialLimit 表示该玩家的独立密码条数已达上限。
	ErrCredentialLimit = errors.New("too many launcher credentials")
	// ErrAccountDisabled 表示账号被封禁/停用（不进服、不给会话）。
	ErrAccountDisabled = errors.New("account is disabled")
	// ErrPasswordLoginDisabled 表示管理员已关闭主密码登录，且该账号已绑定第三方（无宽限可用）。
	ErrPasswordLoginDisabled = errors.New("password login is disabled")
	// ErrBindingRequired 表示管理员要求先绑定第三方账号才能使用该功能。
	ErrBindingRequired = errors.New("third-party binding is required")
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
	// TokenOAuthLogin 是第三方 OAuth 回跳后的一次性换取码（短时效、单次消费），
	// 由 web 的服务端路由兑换成会话，避免把会话令牌放进浏览器 URL。
	TokenOAuthLogin = "oauth-login"
)

// Session 是 Yggdrasil 会话。
type Session struct {
	UserID string
	// PlayerID 空串 = 网站/OIDC 会话，不代表任何玩家（也就不能 join 服务器）。
	PlayerID string
	// CredentialID 是建立本会话的启动器独立密码；空 = 非启动器会话。
	// 撤销该凭证时数据库按外键级联销毁会话，启动器立刻掉线。
	CredentialID string
	AccessToken  string
	ClientToken  string
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

// PlayerCredential 是一条启动器独立密码（对外永不含哈希）。
// 一个玩家可以有多条，按设备/用途区分；撤销即删行。
type PlayerCredential struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// Identity 是一条第三方账号绑定。
type Identity struct {
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	Email     string `json:"email"`
	CreatedAt string `json:"created_at"`
}

// LauncherAuth 是一次启动器认证的结果（玩家 + 归属账号 + 命中的凭证）。
type LauncherAuth struct {
	Player       Player
	UserID       string
	CredentialID string
}

// AuthPolicy 是管理员可调的认证策略。
// PasswordLogin 由 login_methods 的 password 开关推导（不单独持久化，避免两个开关同一件事）；
// 其余两项存 settings 的 auth_policy 行。
//
// PasswordLogin=false 时主密码仍对「尚未绑定第三方」的历史账号放行（宽限），
// 绑定过的账号一律拒绝。于是纯密码账号归零的那一刻，主密码登录自然完全失效，
// 不需要管理员再翻第二个开关，也不需要在登录热路径上统计全局人数。
type AuthPolicy struct {
	PasswordLogin  bool `json:"-"`
	RequireBinding bool `json:"require_third_party_binding"`
	MaxPlayers     int  `json:"max_players"`
}

// AuthPolicySource 提供生效中的认证策略，由 admin.Service 实现
// （yggdrasil 不能反向依赖 admin，故在此定义接口）。
type AuthPolicySource interface {
	AuthPolicy(ctx context.Context) (AuthPolicy, error)
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
	// Authenticate 校验**网站主密码**（email + users.password_hash）。
	// 启动器协议端点不走这里，见 AuthenticatePlayer。
	Authenticate(ctx context.Context, email, password string) (userID string, err error)
	// AuthenticatePlayer 校验**启动器登录**：玩家名 + 该玩家的独立密码。
	// 归属账号被封禁/停用返回 ErrAccountDisabled；无凭证或密码不符返回 ErrInvalidCredentials。
	AuthenticatePlayer(ctx context.Context, playerName, password string) (*LauncherAuth, error)
	PlayersByUser(ctx context.Context, userID string) ([]Player, error)
	// AddPlayer 新建玩家；limit<=0 表示不限（上限由 AuthPolicy.MaxPlayers 决定）。
	AddPlayer(ctx context.Context, userID, name string, limit int) (*Player, error)
	// RenamePlayer 改名会重算离线 UUID（uuid 由名字派生），并使该玩家的游戏会话/服务器绑定失效。
	// 已绑定正版（premium_uuid 非空）的玩家名字来自 Mojang，实现应拒绝改名。
	RenamePlayer(ctx context.Context, userID, oldName, newName string) (*Player, error)
	// DeletePlayer 删除玩家；实现应拒绝删除账号下最后一个玩家，并连带删除其独立密码。
	DeletePlayer(ctx context.Context, userID, name string) error

	// 启动器独立密码（ADR-0012）
	// CreatePlayerCredential 新增一条独立密码；passwordHash 由调用方用 passwd.Hash 生成。
	CreatePlayerCredential(ctx context.Context, playerUUID, name, passwordHash string) (*PlayerCredential, error)
	// ListPlayerCredentials 列出某玩家的独立密码（不含哈希），最近使用的在前。
	ListPlayerCredentials(ctx context.Context, playerUUID string) ([]PlayerCredential, error)
	// DeletePlayerCredential 撤销一条独立密码，并连带销毁由它建立的会话。
	DeletePlayerCredential(ctx context.Context, playerUUID, credentialID string) error

	// 第三方账号绑定
	UpsertIdentity(ctx context.Context, provider, subject, userID, email string) error
	IdentityUser(ctx context.Context, provider, subject string) (userID string, err error)
	Identities(ctx context.Context, userID string) ([]Identity, error)
	// CountPasswordOnlyUsers 统计「有主密码、无第三方绑定、已验证且状态正常」的账号数，
	// 只服务管理面板的决策提示，不在登录热路径上调用。
	CountPasswordOnlyUsers(ctx context.Context) (int64, error)
	// CreateSession 建会话；playerID/credentialID 均空 = 网站/OIDC 会话（不代表任何玩家）。
	CreateSession(ctx context.Context, userID, playerID, credentialID, clientToken string) (accessToken string, err error)
	Validate(ctx context.Context, accessToken, clientToken string) (*Session, error)
	Refresh(ctx context.Context, accessToken, clientToken string) (newAccessToken string, err error)
	Invalidate(ctx context.Context, accessToken, clientToken string) error
	// Signout 校验玩家名 + 独立密码后销毁该玩家的游戏会话。
	// 协议原文是「注销该账号所有令牌」，但凭证属于单个玩家，
	// 用一条玩家凭证去销毁同账号其他玩家的会话不合理，故只清这一个玩家的。
	Signout(ctx context.Context, playerName, password string) error
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
	ListAudit(ctx context.Context, q string, limit, offset int) ([]AuditEntry, int64, error)
	SetSetting(ctx context.Context, key string, value []byte) error
	GetSetting(ctx context.Context, key string) ([]byte, error)
}
