// Package account 提供用户体系：注册、邮箱验证、找回密码、OAuth、邀请码。
package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/i18n"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/mc"
	"github.com/Mythicove/shared/oauth"
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
)

// Options 是账号体系的配置开关。
type Options struct {
	InviteOnly           bool
	DisableRegistration  bool
	EmailDomainWhitelist []string
	Captcha              captcha.Captcha
	OAuthProviders       map[string]oauth.Provider
}

// Service 是用户体系服务。
type Service struct {
	store   yggdrasil.Store
	mailer  mailer.Mailer
	baseURL string
	l       i18n.Localizer
	locale  string
	opts    Options
}

// NewService 构造用户体系服务。
func NewService(store yggdrasil.Store, m mailer.Mailer, baseURL string) *Service {
	return &Service{store: store, mailer: m, baseURL: baseURL}
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
	Email        string `json:"email"`
	Password     string `json:"password"`
	Nickname     string `json:"nickname"`
	PlayerName   string `json:"playerName"`
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
		Subject: "Verify your Mythic Mirror account",
		Text:    "Verify: " + s.baseURL + "/account/verify?token=" + token,
	})
	return &RegisterResponse{ID: acct.ID}, nil
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
		Subject: "Reset your Mythic Mirror password",
		Text:    "Reset: " + s.baseURL + "/account/reset-password?token=" + token,
	})
	return nil
}

// ResetPasswordRequest 是重置密码请求。
type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
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

// AddPlayerRequest 是添加玩家名请求。
type AddPlayerRequest struct {
	AccessToken string `json:"accessToken"`
	Name        string `json:"name"`
}

// AddPlayer 为当前用户添加玩家名。
func (s *Service) AddPlayer(ctx context.Context, req AddPlayerRequest) (*yggdrasil.Player, error) {
	if !mc.ValidPlayerName(req.Name) {
		return nil, ErrInvalidName
	}
	sess, err := s.store.Validate(ctx, req.AccessToken, "")
	if err != nil {
		return nil, err
	}
	return s.store.AddPlayer(ctx, sess.UserID, req.Name)
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

// PublishSkinRequest 是发布皮肤到皮肤库请求。
type PublishSkinRequest struct {
	AccessToken string `json:"accessToken"`
	PlayerName  string `json:"playerName"`
	Description string `json:"description"`
}

// PublishSkin 把玩家当前皮肤发布到皮肤库。
func (s *Service) PublishSkin(ctx context.Context, req PublishSkinRequest) (*yggdrasil.SkinLibItem, error) {
	sess, err := s.store.Validate(ctx, req.AccessToken, "")
	if err != nil {
		return nil, err
	}
	players, err := s.store.PlayersByUser(ctx, sess.UserID)
	if err != nil {
		return nil, err
	}
	var target *yggdrasil.Player
	for i := range players {
		if players[i].Name == req.PlayerName {
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
	return s.store.PublishSkin(ctx, sess.UserID, req.PlayerName, pd.SkinKey, req.Description, pd.Model)
}

// ListSkinLib 列出皮肤库条目。
func (s *Service) ListSkinLib(ctx context.Context, offset, limit int) ([]yggdrasil.SkinLibItem, error) {
	return s.store.ListSkinLib(ctx, offset, limit)
}

// LikeSkin 点赞皮肤库条目。
func (s *Service) LikeSkin(ctx context.Context, itemID string) error {
	return s.store.LikeSkin(ctx, itemID)
}

// CommentSkinRequest 是评论请求。
type CommentSkinRequest struct {
	AccessToken string `json:"accessToken"`
	ItemID      string `json:"-"`
	Content     string `json:"content"`
}

// CommentSkin 评论皮肤库条目。
func (s *Service) CommentSkin(ctx context.Context, req CommentSkinRequest) error {
	sess, err := s.store.Validate(ctx, req.AccessToken, "")
	if err != nil {
		return err
	}
	return s.store.CommentSkin(ctx, req.ItemID, sess.UserID, req.Content)
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
		out = "user" + randomToken()[:8]
	}
	return out
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
