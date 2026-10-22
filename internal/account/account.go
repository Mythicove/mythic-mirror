// Package account 提供用户体系：注册、邮箱验证、找回密码。
package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/i18n"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/mc"
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
)

// Service 是用户体系服务。
type Service struct {
	store   yggdrasil.Store
	mailer  mailer.Mailer
	baseURL string
	l       i18n.Localizer
	locale  string
}

// SetLocalizer 设置本地化（可选）。
func (s *Service) SetLocalizer(l i18n.Localizer, locale string) {
	s.l = l
	s.locale = locale
}

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

// NewService 构造用户体系服务。
func NewService(store yggdrasil.Store, m mailer.Mailer, baseURL string) *Service {
	return &Service{store: store, mailer: m, baseURL: baseURL}
}

// RegisterRequest 是注册请求。
type RegisterRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	Nickname   string `json:"nickname"`
	PlayerName string `json:"playerName"`
}

// RegisterResponse 是注册响应。
type RegisterResponse struct {
	ID string `json:"id"`
}

// Register 注册新账号并发送验证邮件。
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	if !validEmail(req.Email) {
		return nil, ErrInvalidEmail
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

func validEmail(e string) bool {
	parts := strings.Split(e, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}
