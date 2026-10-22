// Package mojang 提供 Mojang API 客户端（正版验证 + 纹理兜底）。
package mojang

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Resolver 是 Mojang 账号解析抽象。
type Resolver interface {
	ResolveName(ctx context.Context, name string) (string, error)
	ResolveProfile(ctx context.Context, uuid string) (*TextureProfile, error)
}

// TextureProfile 是 Mojang 返回的纹理信息。
type TextureProfile struct {
	SkinURL string
	CapeURL string
	Model   string // slim | default | ""
}

// Client 是 Mojang API 客户端。
type Client struct {
	apiURL     string
	sessionURL string
	http       *http.Client
}

// New 构造指向官方 Mojang API 的客户端。
func New() *Client { return NewWithURL("https://api.mojang.com", "https://sessionserver.mojang.com") }

// NewWithURL 构造指向自定义地址的客户端（测试用，两个端点同源）。
func NewWithURL(apiURL, sessionURL string) *Client {
	return &Client{apiURL: apiURL, sessionURL: sessionURL, http: &http.Client{}}
}

// ResolveName 解析玩家名到 Mojang UUID；不存在时返回错误。
func (c *Client) ResolveName(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/users/profiles/minecraft/"+name, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("mojang: player %q not found", name)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("mojang: status %d: %s", resp.StatusCode, b)
	}
	var v struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.ID, nil
}

// ResolveProfile 拉取正版玩家的纹理（皮肤/披风）。
func (c *Client) ResolveProfile(ctx context.Context, uuid string) (*TextureProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sessionURL+"/session/minecraft/profile/"+uuid, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("mojang: profile %q not found", uuid)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("mojang: status %d: %s", resp.StatusCode, b)
	}
	var v struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	for _, p := range v.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return nil, err
		}
		var t struct {
			Textures struct {
				SKIN struct {
					URL      string `json:"url"`
					Metadata struct {
						Model string `json:"model"`
					} `json:"metadata"`
				} `json:"SKIN"`
				CAPE struct {
					URL string `json:"url"`
				} `json:"CAPE"`
			} `json:"textures"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		return &TextureProfile{
			SkinURL: t.Textures.SKIN.URL,
			CapeURL: t.Textures.CAPE.URL,
			Model:   t.Textures.SKIN.Metadata.Model,
		}, nil
	}
	return &TextureProfile{}, nil
}
