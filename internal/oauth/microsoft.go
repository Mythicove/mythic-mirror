// Package oauth 提供第三方 OAuth 登录实现。
// Microsoft 走完整 Xbox/XSTS/Minecraft 流程，用于"正版验证"绑定。
package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Mythicove/shared/oauth"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"
)

// 可覆盖端点（测试用）。
var (
	xblAuthURL   = "https://user.auth.xboxlive.com/user/authenticate"
	xstsAuthURL  = "https://xsts.auth.xboxlive.com/xsts/authorize"
	mcLoginURL   = "https://api.minecraftservices.com/authentication/login_with_xbox"
	mcProfileURL = "https://api.minecraftservices.com/minecraft/profile"
)

// Microsoft 是 Microsoft 正版验证 provider。
type Microsoft struct {
	conf *oauth2.Config
	http *http.Client
}

var _ oauth.Provider = (*Microsoft)(nil)

// NewMicrosoft 构造 Microsoft provider。
func NewMicrosoft(clientID, clientSecret, redirectURL string) *Microsoft {
	return &Microsoft{
		conf: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"XboxLive.signin"},
			Endpoint:     microsoft.LiveConnectEndpoint,
		},
		http: &http.Client{},
	}
}

// AuthURL 生成授权跳转 URL。
func (m *Microsoft) AuthURL(state string) string {
	return m.conf.AuthCodeURL(state)
}

// Exchange 完成 Microsoft OAuth + Xbox/XSTS/Minecraft 流程，返回 Minecraft profile。
func (m *Microsoft) Exchange(ctx context.Context, code string) (*oauth.Profile, error) {
	tok, err := m.conf.Exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	xbl, err := m.authenticateXBL(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	if len(xbl.DisplayClaims.Xui) == 0 {
		return nil, fmt.Errorf("oauth: no xbl user hash")
	}
	uhs := xbl.DisplayClaims.Xui[0].UHS
	xsts, err := m.authenticateXSTS(ctx, xbl.Token)
	if err != nil {
		return nil, err
	}
	mcToken, err := m.loginMinecraft(ctx, uhs, xsts)
	if err != nil {
		return nil, err
	}
	profile, err := m.getProfile(ctx, mcToken)
	if err != nil {
		return nil, err
	}
	return &oauth.Profile{Provider: "microsoft", UID: profile.ID, Nickname: profile.Name}, nil
}

type xblResponse struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		Xui []struct {
			UHS string `json:"uhs"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (m *Microsoft) authenticateXBL(ctx context.Context, msToken string) (*xblResponse, error) {
	body := map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + msToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	var resp xblResponse
	if err := postJSON(ctx, m.http, xblAuthURL, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (m *Microsoft) authenticateXSTS(ctx context.Context, xblToken string) (string, error) {
	body := map[string]any{
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}
	var resp struct {
		Token string `json:"Token"`
		XErr  int64  `json:"XErr"`
	}
	if err := postJSON(ctx, m.http, xstsAuthURL, body, &resp); err != nil {
		return "", err
	}
	if resp.XErr != 0 {
		return "", fmt.Errorf("oauth: xsts XErr=%d", resp.XErr)
	}
	return resp.Token, nil
}

func (m *Microsoft) loginMinecraft(ctx context.Context, uhs, xstsToken string) (string, error) {
	body := map[string]any{
		"identityToken": "XBL3.0 x=" + uhs + ";" + xstsToken,
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := postJSON(ctx, m.http, mcLoginURL, body, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("oauth: minecraft login %s", resp.Error)
	}
	return resp.AccessToken, nil
}

type mcProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *Microsoft) getProfile(ctx context.Context, mcToken string) (*mcProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcProfileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+mcToken)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: minecraft profile status %d", resp.StatusCode)
	}
	var p mcProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, fmt.Errorf("oauth: empty minecraft profile")
	}
	return &p, nil
}

func postJSON(ctx context.Context, client *http.Client, url string, body any, dst any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oauth: status %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
