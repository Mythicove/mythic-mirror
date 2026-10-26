// Package oidc 提供 OIDC Provider（Ory Fosite）：授权码 + PKCE、client_credentials、refresh、用户信息。
package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"github.com/ory/fosite/storage"
	"github.com/ory/fosite/token/jwt"
)

const settingsKey = "oauth_clients"

// OAuthClient 是 OIDC 客户端（持久化于 settings）。
type OAuthClient struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	SecretHash   string   `json:"secret_hash,omitempty"` // sha256 hex；public 客户端为空
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	Scopes       []string `json:"scopes"`
	Public       bool     `json:"public"`
	// FirstParty 标记同部署第一方服务（如 Hoard）：授权时免同意页。
	FirstParty bool `json:"first_party,omitempty"`
}

// SessionPersister 是 fosite 会话类存储的持久化抽象（授权码/access/refresh/PKCE/OIDC 会话）。
// Postgres 实现见 internal/pgstore.OIDCStore；nil 表示进程内内存（单实例/开发）。
type SessionPersister interface {
	CreateAuthorizeCodeSession(ctx context.Context, code string, req fosite.Requester) error
	GetAuthorizeCodeSession(ctx context.Context, code string, sess fosite.Session) (fosite.Requester, error)
	InvalidateAuthorizeCodeSession(ctx context.Context, code string) error
	CreateAccessTokenSession(ctx context.Context, signature string, req fosite.Requester) error
	GetAccessTokenSession(ctx context.Context, signature string, sess fosite.Session) (fosite.Requester, error)
	DeleteAccessTokenSession(ctx context.Context, signature string) error
	CreateRefreshTokenSession(ctx context.Context, signature, accessSignature string, req fosite.Requester) error
	GetRefreshTokenSession(ctx context.Context, signature string, sess fosite.Session) (fosite.Requester, error)
	DeleteRefreshTokenSession(ctx context.Context, signature string) error
	RevokeRefreshToken(ctx context.Context, requestID string) error
	RevokeAccessToken(ctx context.Context, requestID string) error
	RotateRefreshToken(ctx context.Context, requestID, signature string) error
	CreatePKCERequestSession(ctx context.Context, code string, req fosite.Requester) error
	GetPKCERequestSession(ctx context.Context, code string, sess fosite.Session) (fosite.Requester, error)
	DeletePKCERequestSession(ctx context.Context, code string) error
	CreateOpenIDConnectSession(ctx context.Context, code string, req fosite.Requester) error
	GetOpenIDConnectSession(ctx context.Context, code string, req fosite.Requester) (fosite.Requester, error)
	DeleteOpenIDConnectSession(ctx context.Context, code string) error
}

// compositeStorage：客户端仍由内存 MemoryStore 承载（settings 驱动，ReloadClients 刷新），
// 会话类存储委托给 SessionPersister（nil 时回落内存）。
type compositeStorage struct {
	*storage.MemoryStore
	p         SessionPersister
	getClient func(ctx context.Context, id string) (fosite.Client, error)
}

// GetClient 走 Provider 的钩子：内存未命中时节流重载 settings，
// 使其他副本注册的客户端近实时可见（多实例一致性）。
func (c *compositeStorage) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	if c.getClient != nil {
		return c.getClient(ctx, id)
	}
	return c.MemoryStore.GetClient(ctx, id)
}

func (c *compositeStorage) CreateAuthorizeCodeSession(ctx context.Context, code string, req fosite.Requester) error {
	if c.p != nil {
		return c.p.CreateAuthorizeCodeSession(ctx, code, req)
	}
	return c.MemoryStore.CreateAuthorizeCodeSession(ctx, code, req)
}

func (c *compositeStorage) GetAuthorizeCodeSession(ctx context.Context, code string, sess fosite.Session) (fosite.Requester, error) {
	if c.p != nil {
		return c.p.GetAuthorizeCodeSession(ctx, code, sess)
	}
	return c.MemoryStore.GetAuthorizeCodeSession(ctx, code, sess)
}

func (c *compositeStorage) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	if c.p != nil {
		return c.p.InvalidateAuthorizeCodeSession(ctx, code)
	}
	return c.MemoryStore.InvalidateAuthorizeCodeSession(ctx, code)
}

func (c *compositeStorage) CreateAccessTokenSession(ctx context.Context, signature string, req fosite.Requester) error {
	if c.p != nil {
		return c.p.CreateAccessTokenSession(ctx, signature, req)
	}
	return c.MemoryStore.CreateAccessTokenSession(ctx, signature, req)
}

func (c *compositeStorage) GetAccessTokenSession(ctx context.Context, signature string, sess fosite.Session) (fosite.Requester, error) {
	if c.p != nil {
		return c.p.GetAccessTokenSession(ctx, signature, sess)
	}
	return c.MemoryStore.GetAccessTokenSession(ctx, signature, sess)
}

func (c *compositeStorage) DeleteAccessTokenSession(ctx context.Context, signature string) error {
	if c.p != nil {
		return c.p.DeleteAccessTokenSession(ctx, signature)
	}
	return c.MemoryStore.DeleteAccessTokenSession(ctx, signature)
}

func (c *compositeStorage) CreateRefreshTokenSession(ctx context.Context, signature, accessSignature string, req fosite.Requester) error {
	if c.p != nil {
		return c.p.CreateRefreshTokenSession(ctx, signature, accessSignature, req)
	}
	return c.MemoryStore.CreateRefreshTokenSession(ctx, signature, accessSignature, req)
}

func (c *compositeStorage) GetRefreshTokenSession(ctx context.Context, signature string, sess fosite.Session) (fosite.Requester, error) {
	if c.p != nil {
		return c.p.GetRefreshTokenSession(ctx, signature, sess)
	}
	return c.MemoryStore.GetRefreshTokenSession(ctx, signature, sess)
}

func (c *compositeStorage) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	if c.p != nil {
		return c.p.DeleteRefreshTokenSession(ctx, signature)
	}
	return c.MemoryStore.DeleteRefreshTokenSession(ctx, signature)
}

func (c *compositeStorage) RevokeRefreshToken(ctx context.Context, requestID string) error {
	if c.p != nil {
		return c.p.RevokeRefreshToken(ctx, requestID)
	}
	return c.MemoryStore.RevokeRefreshToken(ctx, requestID)
}

func (c *compositeStorage) RevokeAccessToken(ctx context.Context, requestID string) error {
	if c.p != nil {
		return c.p.RevokeAccessToken(ctx, requestID)
	}
	return c.MemoryStore.RevokeAccessToken(ctx, requestID)
}

func (c *compositeStorage) RotateRefreshToken(ctx context.Context, requestID, signature string) error {
	if c.p != nil {
		return c.p.RotateRefreshToken(ctx, requestID, signature)
	}
	return c.MemoryStore.RotateRefreshToken(ctx, requestID, signature)
}

func (c *compositeStorage) CreatePKCERequestSession(ctx context.Context, code string, req fosite.Requester) error {
	if c.p != nil {
		return c.p.CreatePKCERequestSession(ctx, code, req)
	}
	return c.MemoryStore.CreatePKCERequestSession(ctx, code, req)
}

func (c *compositeStorage) GetPKCERequestSession(ctx context.Context, code string, sess fosite.Session) (fosite.Requester, error) {
	if c.p != nil {
		return c.p.GetPKCERequestSession(ctx, code, sess)
	}
	return c.MemoryStore.GetPKCERequestSession(ctx, code, sess)
}

func (c *compositeStorage) DeletePKCERequestSession(ctx context.Context, code string) error {
	if c.p != nil {
		return c.p.DeletePKCERequestSession(ctx, code)
	}
	return c.MemoryStore.DeletePKCERequestSession(ctx, code)
}

func (c *compositeStorage) CreateOpenIDConnectSession(ctx context.Context, code string, req fosite.Requester) error {
	if c.p != nil {
		return c.p.CreateOpenIDConnectSession(ctx, code, req)
	}
	return c.MemoryStore.CreateOpenIDConnectSession(ctx, code, req)
}

func (c *compositeStorage) GetOpenIDConnectSession(ctx context.Context, code string, req fosite.Requester) (fosite.Requester, error) {
	if c.p != nil {
		return c.p.GetOpenIDConnectSession(ctx, code, req)
	}
	return c.MemoryStore.GetOpenIDConnectSession(ctx, code, req)
}

func (c *compositeStorage) DeleteOpenIDConnectSession(ctx context.Context, code string) error {
	if c.p != nil {
		return c.p.DeleteOpenIDConnectSession(ctx, code)
	}
	return c.MemoryStore.DeleteOpenIDConnectSession(ctx, code)
}

// Provider 是 OIDC 服务。
type Provider struct {
	provider        fosite.OAuth2Provider
	store           yggdrasil.Store
	mem             *storage.MemoryStore
	baseURL         string
	webURL          string
	rsaKey          *keys.KeyPair
	config          *fosite.Config
	keyGetter       func(context.Context) (interface{}, error)
	adminTokenValue string

	reloadMu   sync.Mutex
	lastReload time.Time
}

// clientReloadInterval 是"未命中→重载 settings"的最小间隔（防止无效 client_id 打爆数据库）。
const clientReloadInterval = time.Second

// NewProvider 构造 OIDC Provider；persister 为 nil 时会话存进程内存（单实例/开发）。
func NewProvider(store yggdrasil.Store, kp *keys.KeyPair, baseURL string, persister SessionPersister) *Provider {
	keyGetter := func(context.Context) (interface{}, error) { return kp.PrivateKey(), nil }

	cfg := &fosite.Config{
		AccessTokenLifespan:   time.Hour,
		RefreshTokenLifespan:  time.Hour * 24 * 30,
		AuthorizeCodeLifespan: 15 * time.Minute,
		IDTokenLifespan:       time.Hour,
		IDTokenIssuer:         baseURL,
		TokenURL:              baseURL + "/oauth2/token",
		AllowedPromptValues:   []string{"login", "consent", "none"},
		ClientSecretsHasher:   sha256Hasher{},
		GlobalSecret:          deriveSecret(kp),
	}

	p := &Provider{
		store:     store,
		mem:       storage.NewMemoryStore(),
		baseURL:   baseURL,
		rsaKey:    kp,
		config:    cfg,
		keyGetter: keyGetter,
	}
	p.provider = compose.Compose(
		cfg,
		&compositeStorage{MemoryStore: p.mem, p: persister, getClient: p.getClientThrottled},
		&compose.CommonStrategy{
			CoreStrategy:               compose.NewOAuth2HMACStrategy(cfg),
			OpenIDConnectTokenStrategy: compose.NewOpenIDConnectStrategy(keyGetter, cfg),
			Signer:                     &jwt.DefaultSigner{GetPrivateKey: keyGetter},
		},
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2ClientCredentialsGrantFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2PKCEFactory,
		compose.OpenIDConnectExplicitFactory,
		compose.OpenIDConnectRefreshFactory,
		compose.OAuth2TokenIntrospectionFactory,
		compose.OAuth2TokenRevocationFactory,
	)
	return p
}

// getClientThrottled 先查内存；未命中则（节流地）从 settings 重载后重查，
// 让其他副本新注册的客户端在近实时内可见。
func (p *Provider) getClientThrottled(ctx context.Context, id string) (fosite.Client, error) {
	if cli, err := p.mem.GetClient(ctx, id); err == nil {
		return cli, nil
	}
	p.reloadMu.Lock()
	if time.Since(p.lastReload) >= clientReloadInterval {
		p.lastReload = time.Now()
		p.reloadMu.Unlock()
		if err := p.ReloadClients(ctx); err != nil {
			return nil, err
		}
	} else {
		p.reloadMu.Unlock()
	}
	return p.mem.GetClient(ctx, id)
}

// ReloadClients 从 settings 重新加载 OIDC 客户端。
func (p *Provider) ReloadClients(ctx context.Context) error {
	clients, err := p.ListClients(ctx)
	if err != nil {
		return err
	}
	p.mem.Clients = map[string]fosite.Client{}
	for _, c := range clients {
		p.mem.Clients[c.ID] = c.fositeClient()
	}
	return nil
}

// ListClients 返回全部客户端。
func (p *Provider) ListClients(ctx context.Context) ([]OAuthClient, error) {
	raw, err := p.store.GetSetting(ctx, settingsKey)
	if err != nil || len(raw) == 0 {
		return []OAuthClient{}, nil
	}
	var clients []OAuthClient
	if err := json.Unmarshal(raw, &clients); err != nil {
		return nil, err
	}
	return clients, nil
}

// SaveClient 创建或更新客户端，并刷新内存。
func (p *Provider) SaveClient(ctx context.Context, c OAuthClient) error {
	if c.ID == "" {
		return fosite.ErrInvalidClient
	}
	clients, err := p.ListClients(ctx)
	if err != nil {
		return err
	}
	replaced := false
	for i := range clients {
		if clients[i].ID == c.ID {
			clients[i] = c
			replaced = true
		}
	}
	if !replaced {
		clients = append(clients, c)
	}
	b, err := json.Marshal(clients)
	if err != nil {
		return err
	}
	if err := p.store.SetSetting(ctx, settingsKey, b); err != nil {
		return err
	}
	return p.ReloadClients(ctx)
}

// DeleteClient 删除客户端。
func (p *Provider) DeleteClient(ctx context.Context, id string) error {
	clients, err := p.ListClients(ctx)
	if err != nil {
		return err
	}
	out := make([]OAuthClient, 0, len(clients))
	for _, c := range clients {
		if c.ID != id {
			out = append(out, c)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := p.store.SetSetting(ctx, settingsKey, b); err != nil {
		return err
	}
	return p.ReloadClients(ctx)
}

// HashClientSecret 返回客户端密钥的 sha256 hex。
func HashClientSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (c OAuthClient) fositeClient() fosite.Client {
	secret, _ := hex.DecodeString(c.SecretHash)
	return &fosite.DefaultClient{
		ID:            c.ID,
		Secret:        secret,
		RedirectURIs:  c.RedirectURIs,
		GrantTypes:    fosite.Arguments(c.GrantTypes),
		ResponseTypes: fosite.Arguments{"code"},
		Scopes:        fosite.Arguments(c.Scopes),
		Public:        c.Public,
	}
}

type sha256Hasher struct{}

func (sha256Hasher) Hash(_ context.Context, data []byte) ([]byte, error) {
	sum := sha256.Sum256(data)
	return sum[:], nil
}

func (sha256Hasher) Compare(_ context.Context, hash, data []byte) error {
	sum := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(hash, sum[:]) == 1 {
		return nil
	}
	return fosite.ErrInvalidClient
}

func deriveSecret(kp *keys.KeyPair) []byte {
	sum := sha256.Sum256(kp.PrivateKey().D.Bytes())
	return sum[:]
}
