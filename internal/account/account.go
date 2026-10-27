// Package account 提供用户体系：注册、邮箱验证、找回密码、OAuth、邀请码。
package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/cache"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/i18n"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/mc"
	"github.com/Mythicove/shared/oauth"
	"github.com/google/uuid"
)

// MinPasswordLength 是密码最小长度。注册/重置的校验、错误文案与
// GET /api/register-requirements 的对外声明都以它为准，避免多处各写一个 8 而漂移。
const MinPasswordLength = 8

var (
	// ErrInvalidEmail 表示邮箱格式错误。
	ErrInvalidEmail = errors.New("invalid email")
	// ErrWeakPassword 表示密码过短。
	ErrWeakPassword = errors.New("password must be at least 8 characters")
	// ErrInvalidName 表示玩家名非法。
	ErrInvalidName = errors.New("invalid player name")
	// ErrNameTaken 表示玩家名已被占用。
	ErrNameTaken = errors.New("player name already taken")
	// ErrRegistrationDisabled 表示注册已关闭。
	ErrRegistrationDisabled = errors.New("registration disabled")
	// ErrCaptchaFailed 表示验证码校验失败。
	ErrCaptchaFailed = errors.New("captcha failed")
	// ErrCaptchaUnavailable 表示运维配置了验证码驱动但配置不完整（如 cap 缺 secret/实例地址），
	// 验证码实际未生效。此时拒绝注册而不是静默无保护放行。
	ErrCaptchaUnavailable = errors.New("captcha is configured but unavailable")
	// ErrInviteRequired 表示需要邀请码。
	ErrInviteRequired = errors.New("invitation code required")
	// ErrInvalidInvite 表示邀请码无效。
	ErrInvalidInvite = errors.New("invalid invitation code")
	// ErrUnknownProvider 表示未知 OAuth provider。
	ErrUnknownProvider = errors.New("unknown oauth provider")
	// ErrInvalidState 表示 OAuth state 无效。
	ErrInvalidState = errors.New("invalid oauth state")
	// ErrInvalidComment 表示评论内容为空或过长。
	ErrInvalidComment = errors.New("invalid comment")
	// ErrInvalidReport 表示举报输入非法（目标类型/理由长度）。
	ErrInvalidReport = errors.New("invalid report")
)

// Options 是账号体系的配置开关。
type Options struct {
	InviteOnly           bool
	DisableRegistration  bool
	EmailDomainWhitelist []string
	Captcha              captcha.Captcha
	CaptchaDriver        string // turnstile | recaptcha | hcaptcha | cap | ""（前端据此渲染控件）
	CaptchaSiteKey       string // 验证码公钥（前端可见）
	CaptchaCapURL        string // cap driver 专用：Standalone 实例公网地址（前端加载控件用）
	OAuthProviders       map[string]oauth.Provider
	AdminToken           string
	MojangOAuth          oauth.Provider // Microsoft 正版验证
	WebURL               string         // 前端站点地址（邮件链接用）；为空时退回 BaseURL
	SkinReview           bool           // 开启后新发布皮肤进入 under_review 待审核
}

// Service 是用户体系服务。
type Service struct {
	store   yggdrasil.Store
	mailer  mailer.Mailer
	baseURL string
	l       i18n.Localizer
	locale  string
	opts    Options
	cache   cache.Cache // Microsoft 绑定 state 等短期状态（多副本安全）
	quota   yggdrasil.Quota
}

// SetQuota 设置配额（发布数量限制；零值不限制）。
func (s *Service) SetQuota(q yggdrasil.Quota) { s.quota = q }

// mojangState 是 Microsoft 正版绑定的中间状态。
type mojangState struct {
	UserID string `json:"user_id"`
}

// stateTTL 是绑定 state 的有效期。
const stateTTL = 10 * time.Minute

// NewService 构造用户体系服务；c 用于跨副本共享短期状态（如 Microsoft 绑定 state）。
func NewService(store yggdrasil.Store, m mailer.Mailer, baseURL string, c cache.Cache) *Service {
	return &Service{store: store, mailer: m, baseURL: baseURL, cache: c}
}

// SetLocalizer 设置本地化（可选）。
func (s *Service) SetLocalizer(l i18n.Localizer, locale string) {
	s.l = l
	s.locale = locale
}

// Configure 设置功能开关与 provider。
func (s *Service) Configure(opts Options) { s.opts = opts }

// t 翻译消息键；未设置本地化时返回键本身。
func (s *Service) t(key string, args map[string]any) string {
	if s.l == nil {
		return key
	}
	locale := s.locale
	if locale == "" {
		locale = "en-US"
	}
	return s.l.T(context.Background(), locale, key, args)
}

// RegisterRequest 是注册请求。
type RegisterRequest struct {
	Email string `json:"email" validate:"required,email"`
	// 密码长度由 MinPasswordLength 单点约束（不再写 min=8 标签，避免与常量漂移）
	Password     string `json:"password" validate:"required"`
	Nickname     string `json:"nickname"`
	PlayerName   string `json:"playerName" validate:"required"`
	CaptchaToken string `json:"captchaToken"`
	InviteCode   string `json:"inviteCode"`
	RemoteIP     string `json:"remoteIP"`
}

// RegisterResponse 是注册响应。
type RegisterResponse struct {
	ID string `json:"id"`
}

// Register 注册新账号并发送验证邮件。
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	if s.opts.DisableRegistration {
		return nil, ErrRegistrationDisabled
	}
	if !validEmail(req.Email) || !s.emailAllowed(req.Email) {
		return nil, ErrInvalidEmail
	}
	// 配置了驱动却没构造出验证码实例 = 配置不完整：宁可拒绝注册，
	// 也不要让"以为有验证码"的部署静默裸奔（fail closed）。
	if s.opts.CaptchaDriver != "" && s.opts.Captcha == nil {
		return nil, ErrCaptchaUnavailable
	}
	if s.opts.Captcha != nil {
		ok, err := s.opts.Captcha.Verify(ctx, req.CaptchaToken, req.RemoteIP)
		if err != nil || !ok {
			return nil, ErrCaptchaFailed
		}
	}
	if s.opts.InviteOnly {
		if req.InviteCode == "" {
			return nil, ErrInviteRequired
		}
		if err := s.store.ConsumeInviteCode(ctx, req.InviteCode); err != nil {
			return nil, ErrInvalidInvite
		}
	}
	if len(req.Password) < MinPasswordLength {
		return nil, ErrWeakPassword
	}
	if !mc.ValidPlayerName(req.PlayerName) {
		return nil, ErrInvalidName
	}
	if _, err := s.store.ProfileByName(ctx, req.PlayerName); err == nil {
		return nil, ErrNameTaken
	}
	hash, err := passwd.Hash(req.Password)
	if err != nil {
		return nil, err
	}
	acct, err := s.store.CreateAccount(ctx, req.Email, hash, req.Nickname, req.PlayerName)
	if err != nil {
		return nil, err
	}
	token, err := s.store.CreateToken(ctx, acct.ID, yggdrasil.TokenVerify, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	if err := s.mailer.Send(ctx, s.composeMail(req.Email, "verify", s.webBase()+"/account/verify?token="+token)); err != nil {
		log.Printf("account: send verify mail to %s: %v", req.Email, err)
	}
	return &RegisterResponse{ID: acct.ID}, nil
}

// composeMail 组装纯文本 + HTML 邮件（共用 intro/button/footer i18n 键）。
func (s *Service) composeMail(to, kind, url string) mailer.Message {
	subject := s.t("account.mail."+kind+".subject", nil)
	intro := s.t("account.mail."+kind+".intro", nil)
	button := s.t("account.mail."+kind+".button", nil)
	footer := s.t("account.mail."+kind+".footer", nil)
	return mailer.Message{
		To:      to,
		Subject: subject,
		Text:    intro + "\n\n" + url + "\n\n" + footer,
		HTML:    renderMailHTML(mailData{Subject: subject, Intro: intro, URL: url, Button: button, Footer: footer}),
	}
}

// webBase 返回邮件链接使用的前端站点地址。
func (s *Service) webBase() string {
	if s.opts.WebURL != "" {
		return strings.TrimRight(s.opts.WebURL, "/")
	}
	return strings.TrimRight(s.baseURL, "/")
}

func (s *Service) emailAllowed(email string) bool {
	wl := s.opts.EmailDomainWhitelist
	if len(wl) == 0 {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	for _, d := range wl {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}

// VerifyEmail 校验邮箱验证 token 并标记已验证。
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.store.ConsumeToken(ctx, yggdrasil.TokenVerify, token)
	if err != nil {
		return err
	}
	return s.store.MarkVerified(ctx, userID)
}

// ForgotPasswordRequest 是找回密码请求。
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword 发送重置邮件（不泄露账号是否存在）。
func (s *Service) ForgotPassword(ctx context.Context, req ForgotPasswordRequest) error {
	if !validEmail(req.Email) {
		return nil
	}
	acct, err := s.store.AccountByEmail(ctx, req.Email)
	if err != nil {
		return nil
	}
	token, err := s.store.CreateToken(ctx, acct.ID, yggdrasil.TokenReset, time.Hour)
	if err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, s.composeMail(req.Email, "reset", s.webBase()+"/account/reset-password?token="+token)); err != nil {
		log.Printf("account: send reset mail to %s: %v", req.Email, err)
	}
	return nil
}

// ResetPasswordRequest 是重置密码请求。
type ResetPasswordRequest struct {
	Token    string `json:"token" validate:"required"`
	Password string `json:"password" validate:"required"` // 长度见 MinPasswordLength
}

// ResetPassword 用重置 token 修改密码。
func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest) error {
	if len(req.Password) < MinPasswordLength {
		return ErrWeakPassword
	}
	userID, err := s.store.ConsumeToken(ctx, yggdrasil.TokenReset, req.Token)
	if err != nil {
		return err
	}
	hash, err := passwd.Hash(req.Password)
	if err != nil {
		return err
	}
	return s.store.UpdatePassword(ctx, userID, hash)
}

// AddPlayer 为当前用户添加玩家名（会话取自 context）。
func (s *Service) AddPlayer(ctx context.Context, name string) (*yggdrasil.Player, error) {
	if !mc.ValidPlayerName(name) {
		return nil, ErrInvalidName
	}
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return nil, yggdrasil.ErrInvalidToken
	}
	return s.store.AddPlayer(ctx, sess.UserID, name)
}

// RenamePlayer 给玩家改名。离线 UUID 由名字派生，故改名会换 uuid 并使该玩家的游戏会话失效
// （启动器需重新登录）；已绑定正版（premium_uuid 非空）的玩家名字来自 Mojang，拒绝本地改名。
func (s *Service) RenamePlayer(ctx context.Context, oldName, newName string) (*yggdrasil.Player, error) {
	if !mc.ValidPlayerName(newName) {
		return nil, ErrInvalidName
	}
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return nil, yggdrasil.ErrInvalidToken
	}
	return s.store.RenamePlayer(ctx, sess.UserID, oldName, newName)
}

// DeletePlayer 删除玩家（连同其会话与服务器绑定）；账号至少保留一个玩家。
func (s *Service) DeletePlayer(ctx context.Context, name string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	return s.store.DeletePlayer(ctx, sess.UserID, name)
}

// ActivatePlayer 把某玩家设为"当前玩家"：建会话时优先用它，而不是硬取第一个。
func (s *Service) ActivatePlayer(ctx context.Context, name string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return err
	}
	for _, p := range players {
		if p.Name == name {
			return s.store.SetActivePlayer(ctx, sess.UserID, p.ID)
		}
	}
	return yggdrasil.ErrNotFound
}

// UnbindPremium 解除玩家的微软正版绑定（清空 premium_uuid）。
// 解绑后该玩家回到离线身份：可以本地改名，贴图不再回退到 Mojang。
func (s *Service) UnbindPremium(ctx context.Context, name string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return err
	}
	for _, p := range players {
		if p.Name == name {
			return s.store.SetPremium(ctx, p.ID, "")
		}
	}
	return yggdrasil.ErrNotFound
}

// EffectiveCaptcha 返回**实际生效**的验证码配置。驱动已配置但实例未构造成功（配置不完整）时
// 三项全空：前端因此不会渲染一个后端根本不校验的控件，与 Register 的 fail-closed 语义一致。
func (s *Service) EffectiveCaptcha() (driver, siteKey, capURL string) {
	if s.opts.Captcha == nil {
		return "", "", ""
	}
	return s.opts.CaptchaDriver, s.opts.CaptchaSiteKey, s.opts.CaptchaCapURL
}

// OAuthAuthURL 返回某 provider 的授权跳转 URL。
func (s *Service) OAuthAuthURL(provider, state string) (string, error) {
	p, ok := s.opts.OAuthProviders[provider]
	if !ok {
		return "", ErrUnknownProvider
	}
	return p.AuthURL(state), nil
}

// oauthAccount 用 provider 回调 code 换取身份，并按邮箱登录或建号（建号即视为已验证）。
func (s *Service) oauthAccount(ctx context.Context, provider, code string) (*yggdrasil.Account, error) {
	p, ok := s.opts.OAuthProviders[provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	profile, err := p.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	email := profile.Email
	if email == "" {
		email = profile.UID + "@" + provider + ".oauth"
	}
	acct, err := s.store.AccountByEmail(ctx, email)
	if err != nil {
		playerName := sanitizePlayerName(profile.Nickname)
		acct, err = s.store.CreateAccount(ctx, email, "", profile.Nickname, playerName)
		if err != nil {
			return nil, err
		}
		_ = s.store.MarkVerified(ctx, acct.ID)
	}
	return acct, nil
}

// OAuthLogin 用回调 code 登录/建号，返回 Yggdrasil accessToken（非浏览器调用方使用）。
func (s *Service) OAuthLogin(ctx context.Context, provider, code string) (string, error) {
	acct, err := s.oauthAccount(ctx, provider, code)
	if err != nil {
		return "", err
	}
	return s.store.CreateSession(ctx, acct.ID, randomToken())
}

// oauthCodeTTL 是一次性换取码的有效期：只需覆盖"回跳 → web 服务端兑换"这一跳。
const oauthCodeTTL = 60 * time.Second

// OAuthLoginCode 用回调 code 登录/建号，但**不**把会话令牌交给浏览器，而是返回一次性换取码。
// 旧实现把 accessToken 放在回跳 URL 的 fragment 里：令牌会留在浏览历史/书签中，
// 且落地页上任何脚本都能读到——而它是一张 24 小时有效的完整会话凭证。
func (s *Service) OAuthLoginCode(ctx context.Context, provider, code string) (string, error) {
	acct, err := s.oauthAccount(ctx, provider, code)
	if err != nil {
		return "", err
	}
	return s.store.CreateToken(ctx, acct.ID, yggdrasil.TokenOAuthLogin, oauthCodeTTL)
}

// OAuthSession 是换取码兑换出的会话。只交给 web 的服务端路由（BFF）设置 httpOnly cookie，
// 不经浏览器 URL。
type OAuthSession struct {
	AccessToken string `json:"accessToken"`
	PlayerName  string `json:"playerName,omitempty"`
	PlayerID    string `json:"playerId,omitempty"`
}

// ExchangeOAuthCode 消费一次性换取码并建会话；码用过即失效（重放 → ErrInvalidToken）。
func (s *Service) ExchangeOAuthCode(ctx context.Context, code string) (*OAuthSession, error) {
	userID, err := s.store.ConsumeToken(ctx, yggdrasil.TokenOAuthLogin, code)
	if err != nil {
		return nil, yggdrasil.ErrInvalidToken
	}
	accessToken, err := s.store.CreateSession(ctx, userID, randomToken())
	if err != nil {
		return nil, err
	}
	out := &OAuthSession{AccessToken: accessToken}
	if p, err := s.store.PlayerByUser(ctx, userID); err == nil && p != nil {
		out.PlayerName, out.PlayerID = p.Name, p.ID
	}
	return out, nil
}

// PublishSkin 把玩家当前皮肤发布到皮肤库（会话取自 context）。
func (s *Service) PublishSkin(ctx context.Context, playerName, description string) (*yggdrasil.SkinLibItem, error) {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return nil, yggdrasil.ErrInvalidToken
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	var target *yggdrasil.Player
	for i := range players {
		if players[i].Name == playerName {
			target = &players[i]
		}
	}
	if target == nil {
		return nil, yggdrasil.ErrNotFound
	}
	pd, err := s.store.ProfileByUUID(ctx, target.ID)
	if err != nil || pd.SkinKey == "" {
		return nil, yggdrasil.ErrNotFound
	}
	if s.quota.Published > 0 {
		n, err := s.store.CountPublished(ctx, sess.UserID)
		if err != nil {
			return nil, err
		}
		if n >= s.quota.Published {
			return nil, yggdrasil.ErrQuotaExceeded
		}
	}
	status := "listed"
	if s.opts.SkinReview {
		status = "under_review"
	}
	return s.store.PublishSkin(ctx, sess.UserID, playerName, pd.SkinKey, description, pd.Model, status)
}

// Report 举报皮肤条目或评论（会话取自 context）；同一目标重复举报返回 ErrAlreadyExists。
func (s *Service) Report(ctx context.Context, targetType, targetID, reason string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	if targetType != "skin" && targetType != "comment" {
		return ErrInvalidReport
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 {
		return ErrInvalidReport
	}
	if targetType == "skin" {
		if _, err := s.store.GetSkinLibItem(ctx, targetID); err != nil {
			return err
		}
	}
	return s.store.ReportTarget(ctx, sess.UserID, targetType, targetID, reason)
}

// ListSkinLib 列出皮肤库条目（q 匹配玩家名/描述；sort=newest|likes）。
func (s *Service) ListSkinLib(ctx context.Context, query, sort string, offset, limit int) ([]yggdrasil.SkinLibItem, int64, error) {
	if sort != "likes" {
		sort = "newest"
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListSkinLib(ctx, strings.TrimSpace(query), sort, offset, limit)
}

// GetSkinLibItem 返回皮肤库条目详情。
func (s *Service) GetSkinLibItem(ctx context.Context, itemID string) (*yggdrasil.SkinLibItem, error) {
	return s.store.GetSkinLibItem(ctx, itemID)
}

// LikeSkin 点赞皮肤库条目。
func (s *Service) LikeSkin(ctx context.Context, itemID string) error {
	return s.store.LikeSkin(ctx, itemID)
}

// maxCommentLen 是评论长度上限。
const maxCommentLen = 1000

// CommentSkin 评论皮肤库条目（会话取自 context）。
func (s *Service) CommentSkin(ctx context.Context, itemID, content string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	content = strings.TrimSpace(content)
	if content == "" || len([]rune(content)) > maxCommentLen {
		return ErrInvalidComment
	}
	if _, err := s.store.GetSkinLibItem(ctx, itemID); err != nil {
		return err
	}
	return s.store.CommentSkin(ctx, itemID, sess.UserID, content)
}

// ListComments 返回条目的评论列表（时间升序）。
func (s *Service) ListComments(ctx context.Context, itemID string) ([]yggdrasil.SkinComment, error) {
	if _, err := s.store.GetSkinLibItem(ctx, itemID); err != nil {
		return nil, err
	}
	return s.store.ListSkinComments(ctx, itemID)
}

// ImportUser 是批量导入的用户。
type ImportUser struct {
	Email      string `json:"email"`
	Nickname   string `json:"nickname"`
	PlayerName string `json:"playerName"`
}

// BatchImportRequest 是批量导入请求。
type BatchImportRequest struct {
	Users []ImportUser `json:"users"`
}

// BatchImport 批量导入用户（跳过无效项，返回成功数）。
func (s *Service) BatchImport(ctx context.Context, req BatchImportRequest) (int, error) {
	count := 0
	for _, u := range req.Users {
		if !validEmail(u.Email) || !mc.ValidPlayerName(u.PlayerName) {
			continue
		}
		hash, _ := passwd.Hash(randomToken())
		if _, err := s.store.CreateAccount(ctx, u.Email, hash, u.Nickname, u.PlayerName); err == nil {
			count++
		}
	}
	return count, nil
}

// MojangOAuthAuthURL 返回 Microsoft 授权 URL（记录 state 对应用户，会话取自 context）。
func (s *Service) MojangOAuthAuthURL(ctx context.Context) (string, error) {
	if s.opts.MojangOAuth == nil {
		return "", ErrUnknownProvider
	}
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return "", yggdrasil.ErrInvalidToken
	}
	state := randomToken()
	b, err := json.Marshal(mojangState{UserID: sess.UserID})
	if err != nil {
		return "", err
	}
	if err := s.cache.Set(ctx, "mojang-state:"+state, b, stateTTL); err != nil {
		return "", err
	}
	return s.opts.MojangOAuth.AuthURL(state), nil
}

// MojangOAuthCallback 用回调 code 完成正版绑定。
func (s *Service) MojangOAuthCallback(ctx context.Context, state, code string) error {
	if s.opts.MojangOAuth == nil {
		return ErrUnknownProvider
	}
	key := "mojang-state:" + state
	b, ok, err := s.cache.Get(ctx, key)
	if err != nil || !ok {
		return ErrInvalidState
	}
	_ = s.cache.Delete(ctx, key)
	var st mojangState
	if err := json.Unmarshal(b, &st); err != nil {
		return ErrInvalidState
	}
	profile, err := s.opts.MojangOAuth.Exchange(ctx, code)
	if err != nil {
		return err
	}
	pd, err := s.store.ProfileByName(ctx, profile.Nickname)
	if err != nil {
		return yggdrasil.ErrNotFound
	}
	players, err := s.store.PlayersByUser(ctx, st.UserID)
	if err != nil {
		return err
	}
	for _, p := range players {
		if p.ID == pd.Player.ID {
			return s.store.SetPremium(ctx, pd.Player.ID, profile.UID)
		}
	}
	return yggdrasil.ErrNotFound
}

// validEmail 收紧的邮箱校验：长度上限 + 本地部/域名部字符集 + 真实 TLD，
// 取代早期"含 @ 且域名含 ."的宽松判断。
var emailRe = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$`)

func validEmail(e string) bool {
	if len(e) > 254 {
		return false
	}
	parts := strings.Split(e, "@")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 64 {
		return false
	}
	return emailRe.MatchString(e)
}

func sanitizePlayerName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 16 {
		out = out[:16]
	}
	if len(out) < 3 {
		out = "user" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	}
	return out
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
