// Package mojang 提供 Mojang API 客户端（正版验证）。
package mojang

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Resolver 是玩家名 -> Mojang UUID 的解析抽象。
type Resolver interface {
	ResolveName(ctx context.Context, name string) (string, error)
}

// Client 是 Mojang API 客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// New 构造指向官方 Mojang API 的客户端。
func New() *Client { return NewWithURL("https://api.mojang.com") }

// NewWithURL 构造指向自定义地址的客户端（测试用）。
func NewWithURL(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

// ResolveName 解析玩家名到 Mojang UUID；不存在时返回错误。
func (c *Client) ResolveName(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/users/profiles/minecraft/"+name, nil)
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
