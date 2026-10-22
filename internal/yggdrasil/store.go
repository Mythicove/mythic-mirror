package yggdrasil

import (
	"context"
	"errors"
)

var (
	// ErrInvalidCredentials 表示凭证错误。
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken 表示 token 无效。
	ErrInvalidToken = errors.New("invalid token")
	// ErrNotFound 表示资源不存在。
	ErrNotFound = errors.New("not found")
)

// Session 是 Yggdrasil 会话。
type Session struct {
	UserID      string
	PlayerID    string
	AccessToken string
	ClientToken string
}

// ProfileData 是带纹理的玩家资料。
type ProfileData struct {
	Player   Player
	Textures TexturesValue
}

// Store 是 Yggdrasil 所需的存储抽象。
// M3 时 Postgres 实现可同时满足 shared/auth.Auth（Web/API 层）与本接口（Yggdrasil 层）。
type Store interface {
	Authenticate(ctx context.Context, username, password string) (userID string, err error)
	PlayerByUser(ctx context.Context, userID string) (*Player, error)
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
}
