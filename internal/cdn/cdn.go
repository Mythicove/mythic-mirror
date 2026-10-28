// Package cdn 提供 CDN 缓存刷新实现（Cloudflare 用官方 SDK；Bunny/webhook 手写 HTTP）。
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Mythicove/shared/cdn"
	"github.com/cloudflare/cloudflare-go"
)

// Cloudflare 是 Cloudflare purge_cache 实现（官方 SDK）。
type Cloudflare struct {
	zoneID string
	api    *cloudflare.API
}

var _ cdn.CDN = (*Cloudflare)(nil)

// NewCloudflare 构造 Cloudflare 客户端。
func NewCloudflare(zoneID, apiKey string) *Cloudflare {
	api, _ := cloudflare.NewWithAPIToken(apiKey)
	return &Cloudflare{zoneID: zoneID, api: api}
}

func (c *Cloudflare) Purge(ctx context.Context, paths []string) error {
	if c.api == nil {
		return fmt.Errorf("cloudflare: failed to init client")
	}
	_, err := c.api.PurgeCache(ctx, c.zoneID, cloudflare.PurgeCacheRequest{Files: paths})
	return err
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
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bunny.net/purge?url="+url.QueryEscape(p), nil)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
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
