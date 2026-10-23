// Package account 提供用户体系：注册、邮箱验证、找回密码、OAuth、邀请码。
package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/i18n"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/mc"
	"github.com/Mythicove/shared/oauth"
	"github.com/google/uuid"
)

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
	// ErrInviteRequired 表示需要邀请码。
	ErrInviteRequired = errors.New("invitation code required")
	// ErrInvalidInvite 表示邀请码无效。
	ErrInvalidInvite = errors.New("invalid invitation code")
	// ErrUnknownProvider 表示未知 OAuth provider。
	ErrUnknownProvider = errors.New("unknown oauth provider")
	// ErrInvalidState 表示 OAuth state 无效。
	ErrInvalidState = errors.New("invalid oauth state")
)

// Options 是账号体系的配置开关。
type Options struct {
	InviteOnly           bool
	DisableRegistration  bool
	EmailDomainWhitelist []string
	Captcha              captcha.Captcha
	OAuthProviders       map[string]oauth.Provider
	AdminToken           string
	MojangOAuth          oauth.Provider // Microsoft 正版验证
	WebURL               string         // 前端站点地址（邮件链接用）；为空时退回 BaseURL
}

// Service 是用户体系服务。
type Service struct {
	store        yggdrasil.Store
	mailer       mailer.Mailer
	baseURL      string
	l            i18n.Localizer
	locale       string
	opts         Options
	mu           sync.Mutex
	mojangStates map[string]mojangState
}

type mojangState struct {
	userID    string
	expiresAt time.Time
}

// NewService 构造用户体系服务。
func NewService(store yggdrasil.Store, m mailer.Mailer, baseURL string) *Service {
	return &Service{store: store, mailer: m, baseURL: baseURL, mojangStates: map[string]mojangState{}}
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
	Email        string `json:"email" validate:"required,email"`
	Password     string `json:"password" validate:"required,min=8"`
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
	if len(req.Password) < 8 {
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
	_ = s.mailer.Send(ctx, mailer.Message{
		To:      req.Email,
		Subject: s.t("account.mail.verify.subject", nil),
		Text:    s.t("account.mail.verify.body", map[string]any{"url": s.webBase() + "/account/verify?token=" + token}),
	})
	return &RegisterResponse{ID: acct.ID}, nil
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
	_ = s.mailer.Send(ctx, mailer.Message{
		To:      req.Email,
		Subject: s.t("account.mail.reset.subject", nil),
		Text:    s.t("account.mail.reset.body", map[string]any{"url": s.webBase() + "/account/reset-password?token=" + token}),
	})
	return nil
}

// ResetPasswordRequest 是重置密码请求。
type ResetPasswordRequest struct {
	Token    string `json:"token" validate:"required"`
	Password string `json:"password" validate:"required,min=8"`
}

// ResetPassword 用重置 token 修改密码。
func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest) error {
	if len(req.Password) < 8 {
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

// OAuthAuthURL 返回某 provider 的授权跳转 URL。
func (s *Service) OAuthAuthURL(provider, state string) (string, error) {
	p, ok := s.opts.OAuthProviders[provider]
	if !ok {
		return "", ErrUnknownProvider
	}
	return p.AuthURL(state), nil
}

// OAuthLogin 用回调 code 登录/建号，返回 Yggdrasil accessToken。
func (s *Service) OAuthLogin(ctx context.Context, provider, code string) (string, error) {
	p, ok := s.opts.OAuthProviders[provider]
	if !ok {
		return "", ErrUnknownProvider
	}
	profile, err := p.Exchange(ctx, code)
	if err != nil {
		return "", err
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
			return "", err
		}
		_ = s.store.MarkVerified(ctx, acct.ID)
	}
	return s.store.CreateSession(ctx, acct.ID, randomToken())
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
	return s.store.PublishSkin(ctx, sess.UserID, playerName, pd.SkinKey, description, pd.Model)
}

// ListSkinLib 列出皮肤库条目。
func (s *Service) ListSkinLib(ctx context.Context, offset, limit int) ([]yggdrasil.SkinLibItem, error) {
	return s.store.ListSkinLib(ctx, offset, limit)
}

// LikeSkin 点赞皮肤库条目。
func (s *Service) LikeSkin(ctx context.Context, itemID string) error {
	return s.store.LikeSkin(ctx, itemID)
}

// CommentSkin 评论皮肤库条目（会话取自 context）。
func (s *Service) CommentSkin(ctx context.Context, itemID, content string) error {
	sess, ok := yggdrasil.SessionFromContext(ctx)
	if !ok {
		return yggdrasil.ErrInvalidToken
	}
	return s.store.CommentSkin(ctx, itemID, sess.UserID, content)
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
	s.mu.Lock()
	s.mojangStates[state] = mojangState{userID: sess.UserID, expiresAt: time.Now().Add(10 * time.Minute)}
	s.mu.Unlock()
	return s.opts.MojangOAuth.AuthURL(state), nil
}

// MojangOAuthCallback 用回调 code 完成正版绑定。
func (s *Service) MojangOAuthCallback(ctx context.Context, state, code string) error {
	if s.opts.MojangOAuth == nil {
		return ErrUnknownProvider
	}
	s.mu.Lock()
	st, ok := s.mojangStates[state]
	if ok {
		delete(s.mojangStates, state)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(st.expiresAt) {
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
	players, err := s.store.PlayersByUser(ctx, st.userID)
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

func validEmail(e string) bool {
	parts := strings.Split(e, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
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
