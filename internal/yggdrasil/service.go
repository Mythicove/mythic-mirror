package yggdrasil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/mojang"
	"github.com/Mythicove/shared/cache"
	"github.com/Mythicove/shared/cdn"
	"github.com/Mythicove/shared/ratelimit"
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
	cdn      cdn.CDN
	limiter  ratelimit.Limiter
	policy   AuthPolicySource
	quota    Quota
	opts     ServiceOptions
}

// NewService 构造 Yggdrasil 服务。
func NewService(store Store, kp *keys.KeyPair, files storage.Storage, opts ServiceOptions) *Service {
	return &Service{store: store, keys: kp, files: files, opts: opts, limiter: ratelimit.NewMemory()}
}

// SetLimiter 注入限流器（认证端点防爆破用）；缺省为进程内实现。
func (s *Service) SetLimiter(l ratelimit.Limiter) {
	if l != nil {
		s.limiter = l
	}
}

// SetPolicy 注入认证策略来源（admin.Service 实现）；nil 表示不施加绑定要求。
func (s *Service) SetPolicy(p AuthPolicySource) { s.policy = p }

// SetCache 设置 profile 缓存（可选；nil 表示不缓存）。
func (s *Service) SetCache(c cache.Cache) { s.cache = c }

// SetResolver 设置 Mojang 解析器（正版验证用，可选）。
func (s *Service) SetResolver(r mojang.Resolver) { s.resolver = r }

// SetCDN 设置 CDN 刷新器（可选）。
func (s *Service) SetCDN(c cdn.CDN) { s.cdn = c }

// SetQuota 设置上传配额（零值表示不限制）。
func (s *Service) SetQuota(q Quota) { s.quota = q }

// Quota 返回当前配额配置（发布数量限制由 account.Service 使用）。
func (s *Service) GetQuota() Quota { return s.quota }

// checkStorageQuota 校验用户当前已用存储是否超出其 tier 上限。
// 在纹理登记（RecordTextureSize）之后调用：sha256 去重的重传不增加用量，天然放行。
func (s *Service) checkStorageQuota(ctx context.Context, userID string) error {
	if s.quota.StorageBytes <= 0 && len(s.quota.Tiers) == 0 {
		return nil
	}
	tier, used, err := s.store.UserStorage(ctx, userID)
	if err != nil {
		return err
	}
	limit := s.quota.LimitFor(tier)
	if limit > 0 && used > limit {
		return ErrQuotaExceeded
	}
	return nil
}

// Authenticate 处理启动器登录：玩家名 + 该玩家的独立密码（ADR-0012）。
// 主密码在此完全不参与，管理员因此可以关掉主密码登录而启动器照常工作。
func (s *Service) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	auth, err := s.store.AuthenticatePlayer(ctx, req.Username, req.Password)
	if err != nil {
		return nil, err
	}
	if err := CheckBinding(ctx, s.store, s.policy, auth.UserID); err != nil {
		return nil, err
	}
	clientToken := req.ClientToken
	if clientToken == "" {
		clientToken = randomToken()
	}
	accessToken, err := s.store.CreateSession(ctx, auth.UserID, auth.Player.ID, auth.CredentialID, clientToken)
	if err != nil {
		return nil, err
	}
	// 一条独立密码只证明「这一个玩家」的身份，故可用与选中的 profile 都是它本身：
	// 同账号下别的玩家各有各的密码，不该因为知道其中一条就被列出来（也更贴近进服身份）。
	profiles := []Player{auth.Player}
	resp := &AuthenticateResponse{
		AccessToken:       accessToken,
		ClientToken:       clientToken,
		AvailableProfiles: profiles,
		SelectedProfile:   &profiles[0],
	}
	if req.RequestUser {
		resp.User = &User{ID: auth.UserID}
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

// buildProfile 构造带签名的 profile（把纹理键解析成 URL；正版玩家走 Mojang 纹理兜底）。
func (s *Service) buildProfile(ctx context.Context, pd *ProfileData) (*Profile, error) {
	id := pd.Player.ID
	if pd.PremiumUUID != "" {
		id = pd.PremiumUUID
	}
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
	if pd.PremiumUUID != "" && s.resolver != nil && (tv.SKIN == nil || tv.CAPE == nil) {
		if tp, err := s.resolver.ResolveProfile(ctx, pd.PremiumUUID); err == nil {
			if tv.SKIN == nil && tp.SkinURL != "" {
				model := tp.Model
				if model == "" {
					model = "default"
				}
				tv.SKIN = &TextureInfo{URL: tp.SkinURL, Metadata: &TextureMetadata{Model: model}}
			}
			if tv.CAPE == nil && tp.CapeURL != "" {
				tv.CAPE = &TextureInfo{URL: tp.CapeURL}
			}
		}
	}
	t := Textures{
		Timestamp:   time.Now().UnixMilli(),
		ProfileID:   id,
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
		ID:   id,
		Name: pd.Player.Name,
		Properties: []Property{{
			Name:      "textures",
			Value:     value,
			Signature: sig,
		}},
	}, nil
}
