// Package oidc 提供 OIDC Provider（Ory Fosite）：授权码 + PKCE、client_credentials、refresh、用户信息。
package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
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
}

// Provider 是 OIDC 服务。
type Provider struct {
	provider        fosite.OAuth2Provider
	store           yggdrasil.Store
	mem             *storage.MemoryStore
	baseURL         string
	rsaKey          *keys.KeyPair
	config          *fosite.Config
	keyGetter       func(context.Context) (interface{}, error)
	adminTokenValue string
}

// NewProvider 构造 OIDC Provider。
func NewProvider(store yggdrasil.Store, kp *keys.KeyPair, baseURL string) *Provider {
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

	mem := storage.NewMemoryStore()
	provider := compose.Compose(
		cfg,
		mem,
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

	return &Provider{
		provider:  provider,
		store:     store,
		mem:       mem,
		baseURL:   baseURL,
		rsaKey:    kp,
		config:    cfg,
		keyGetter: keyGetter,
	}
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
