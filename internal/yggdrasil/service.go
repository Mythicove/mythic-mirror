package yggdrasil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/mojang"
	"github.com/Mythicove/shared/cache"
	"github.com/Mythicove/shared/storage"
)

// ServiceOptions 是 Yggdrasil 服务的配置。
type ServiceOptions struct {
	ServerName         string
	ImplementationName string
	SkinDomains        []string
	TexturesBaseURL    string
}

// Service 是 Yggdrasil 服务，串接 Store、签名密钥、对象存储与缓存。
type Service struct {
	store    Store
	keys     *keys.KeyPair
	files    storage.Storage
	cache    cache.Cache
	resolver mojang.Resolver
	opts     ServiceOptions
}

// NewService 构造 Yggdrasil 服务。
func NewService(store Store, kp *keys.KeyPair, files storage.Storage, opts ServiceOptions) *Service {
	return &Service{store: store, keys: kp, files: files, opts: opts}
}

// SetCache 设置 profile 缓存（可选；nil 表示不缓存）。
func (s *Service) SetCache(c cache.Cache) { s.cache = c }

// SetResolver 设置 Mojang 解析器（正版验证用，可选）。
func (s *Service) SetResolver(r mojang.Resolver) { s.resolver = r }

// Authenticate 处理登录。
func (s *Service) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	userID, err := s.store.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		return nil, err
	}
	players, err := s.store.PlayersByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(players) == 0 {
		return nil, ErrNotFound
	}
	selected := players[0]
	clientToken := req.ClientToken
	if clientToken == "" {
		clientToken = randomToken()
	}
	accessToken, err := s.store.CreateSession(ctx, userID, clientToken)
	if err != nil {
		return nil, err
	}
	resp := &AuthenticateResponse{
		AccessToken:       accessToken,
		ClientToken:       clientToken,
		AvailableProfiles: players,
		SelectedProfile:   &selected,
	}
	if req.RequestUser {
		resp.User = &User{ID: userID}
	}
	return resp, nil
}

// Refresh 处理令牌刷新。
func (s *Service) Refresh(ctx context.Context, req RefreshRequest) (*RefreshResponse, error) {
	accessToken, err := s.store.Refresh(ctx, req.AccessToken, req.ClientToken)
	if err != nil {
		return nil, err
	}
	sess, err := s.store.Validate(ctx, accessToken, req.ClientToken)
	if err != nil {
		return nil, err
	}
	resp := &RefreshResponse{AccessToken: accessToken, ClientToken: req.ClientToken}
	if req.SelectedProfile != nil {
		resp.SelectedProfile = req.SelectedProfile
	} else if p, err := s.store.ProfileByUUID(ctx, sess.PlayerID); err == nil {
		resp.SelectedProfile = &p.Player
	}
	if req.RequestUser {
		resp.User = &User{ID: sess.UserID}
	}
	return resp, nil
}

// Validate 校验令牌。
func (s *Service) Validate(ctx context.Context, req ValidateRequest) error {
	_, err := s.store.Validate(ctx, req.AccessToken, req.ClientToken)
	return err
}

// Invalidate 使令牌失效。
func (s *Service) Invalidate(ctx context.Context, req InvalidateRequest) error {
	return s.store.Invalidate(ctx, req.AccessToken, req.ClientToken)
}

// Signout 退出登录。
func (s *Service) Signout(ctx context.Context, req SignoutRequest) error {
	return s.store.Signout(ctx, req.Username, req.Password)
}

// Join 记录 serverId 会话。
func (s *Service) Join(ctx context.Context, req JoinRequest) error {
	return s.store.BindServer(ctx, req.AccessToken, req.SelectedProfile, req.ServerID)
}

// HasJoined 服务端换取玩家资料。
func (s *Service) HasJoined(ctx context.Context, username, serverID string) (*Profile, error) {
	pd, err := s.store.HasJoined(ctx, username, serverID)
	if err != nil {
		return nil, err
	}
	return s.profileFor(ctx, pd)
}

// ProfileByUUID 按 UUID 取 profile。
func (s *Service) ProfileByUUID(ctx context.Context, uuid string) (*Profile, error) {
	pd, err := s.store.ProfileByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}
	return s.profileFor(ctx, pd)
}

// LookupNames 批量 name -> UUID。
func (s *Service) LookupNames(ctx context.Context, names []string) ([]Player, error) {
	return s.store.LookupNames(ctx, names)
}

// Meta 返回根 meta 端点数据。
func (s *Service) Meta() map[string]any {
	return map[string]any{
		"meta": map[string]any{
			"serverName":            s.opts.ServerName,
			"implementationName":    s.opts.ImplementationName,
			"implementationVersion": "0.0.1",
		},
		"skinDomains":        s.opts.SkinDomains,
		"signaturePublickey": s.keys.PublicKeyPEM(),
	}
}

// profileFor 构造带签名的 profile，命中缓存则直接返回。
func (s *Service) profileFor(ctx context.Context, pd *ProfileData) (*Profile, error) {
	key := "profile:" + pd.Player.ID
	if s.cache != nil {
		if b, ok, _ := s.cache.Get(ctx, key); ok {
			var p Profile
			if json.Unmarshal(b, &p) == nil {
				return &p, nil
			}
		}
	}
	p, err := s.buildProfile(ctx, pd)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		if b, err := json.Marshal(p); err == nil {
			_ = s.cache.Set(ctx, key, b, 60*time.Second)
		}
	}
	return p, nil
}

// invalidateProfile 清除某玩家的 profile 缓存。
func (s *Service) invalidateProfile(ctx context.Context, uuid string) {
	if s.cache != nil {
		_ = s.cache.Delete(ctx, "profile:"+uuid)
	}
}

// buildProfile 构造带签名的 profile（把纹理键解析成 URL）。
func (s *Service) buildProfile(ctx context.Context, pd *ProfileData) (*Profile, error) {
	tv := TexturesValue{}
	if pd.SkinKey != "" {
		u, err := s.files.PresignGet(ctx, pd.SkinKey)
		if err != nil {
			return nil, err
		}
		model := pd.Model
		if model == "" {
			model = "default"
		}
		tv.SKIN = &TextureInfo{URL: u.String(), Metadata: &TextureMetadata{Model: model}}
	}
	if pd.CapeKey != "" {
		u, err := s.files.PresignGet(ctx, pd.CapeKey)
		if err != nil {
			return nil, err
		}
		tv.CAPE = &TextureInfo{URL: u.String()}
	}
	t := Textures{
		Timestamp:   time.Now().UnixMilli(),
		ProfileID:   pd.Player.ID,
		ProfileName: pd.Player.Name,
		Textures:    tv,
	}
	raw, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	value := base64.StdEncoding.EncodeToString(raw)
	sig, err := s.keys.Sign(value)
	if err != nil {
		return nil, err
	}
	return &Profile{
		ID:   pd.Player.ID,
		Name: pd.Player.Name,
		Properties: []Property{{
			Name:      "textures",
			Value:     value,
			Signature: sig,
		}},
	}, nil
}
