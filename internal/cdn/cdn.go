// Package cdn 提供 CDN 缓存刷新实现（Cloudflare / Bunny / 通用 webhook）。
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Mythicove/shared/cdn"
)

// Cloudflare 是 Cloudflare purge_cache 实现。
type Cloudflare struct {
	zoneID  string
	apiKey  string
	baseURL string
	http    *http.Client
}

var _ cdn.CDN = (*Cloudflare)(nil)

// NewCloudflare 构造 Cloudflare 客户端。
func NewCloudflare(zoneID, apiKey string) *Cloudflare {
	return &Cloudflare{zoneID: zoneID, apiKey: apiKey, baseURL: "https://api.cloudflare.com/client/v4", http: &http.Client{}}
}

func (c *Cloudflare) Purge(ctx context.Context, paths []string) error {
	b, _ := json.Marshal(map[string]any{"files": paths})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/zones/"+c.zoneID+"/purge_cache", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare: purge status %d", resp.StatusCode)
	}
	return nil
}

// Bunny 是 Bunny.net purge 实现。
type Bunny struct {
	apiKey string
	http   *http.Client
}

var _ cdn.CDN = (*Bunny)(nil)

// NewBunny 构造 Bunny 客户端。
func NewBunny(apiKey string) *Bunny {
	return &Bunny{apiKey: apiKey, http: &http.Client{}}
}

func (b *Bunny) Purge(ctx context.Context, paths []string) error {
	for _, p := range paths {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bunny.net/purge?url="+p, nil)
		req.Header.Set("AccessKey", b.apiKey)
		resp, err := b.http.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("bunny: purge status %d", resp.StatusCode)
		}
	}
	return nil
}

// Webhook 是通用 webhook 实现（自建 CDN/网关）。
type Webhook struct {
	url  string
	http *http.Client
}

var _ cdn.CDN = (*Webhook)(nil)

// NewWebhook 构造 webhook 客户端。
func NewWebhook(url string) *Webhook {
	return &Webhook{url: url, http: &http.Client{}}
}

func (w *Webhook) Purge(ctx context.Context, paths []string) error {
	b, _ := json.Marshal(map[string]any{"paths": paths})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: purge status %d", resp.StatusCode)
	}
	return nil
}
