// Package oauth 提供第三方 OAuth 登录实现（GitHub 打样，其余 provider 按需补齐）。
package oauth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Mythicove/shared/oauth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

// GitHub 是 GitHub OAuth provider。
type GitHub struct {
	conf *oauth2.Config
}

var _ oauth.Provider = (*GitHub)(nil)

// NewGitHub 构造 GitHub provider。
func NewGitHub(clientID, clientSecret, redirectURL string) *GitHub {
	return &GitHub{conf: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read:user", "user:email"},
		Endpoint:     github.Endpoint,
	}}
}

// AuthURL 生成授权跳转 URL。
func (g *GitHub) AuthURL(state string) string {
	return g.conf.AuthCodeURL(state)
}

// Exchange 用回调 code 换取用户资料。
func (g *GitHub) Exchange(ctx context.Context, code string) (*oauth.Profile, error) {
	tok, err := g.conf.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	resp, err := g.conf.Client(ctx, tok).Get("https://api.github.com/user")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var u struct {
		ID    int    `json:"id"`
		Login string `json:"login"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, fmt.Errorf("oauth: empty github user")
	}
	return &oauth.Profile{
		Provider: "github",
		UID:      fmt.Sprint(u.ID),
		Email:    u.Email,
		Nickname: u.Login,
	}, nil
}
