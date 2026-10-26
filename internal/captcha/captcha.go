// Package captcha 提供基于 HTTP siteverify 的验证码实现。
// Turnstile / reCAPTCHA / hCaptcha 验证协议一致（POST siteverify，返回 {success}），
// 故用一个实现 + 不同 verify URL 覆盖多个服务商。
// Cap（自托管，https://github.com/tiagozip/cap）的 Standalone 模式提供
// reCAPTCHA 兼容的 siteverify，但按官方文档走 JSON 请求体。
package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Mythicove/shared/captcha"
)

// Client 是通用 siteverify 验证码客户端。
type Client struct {
	verifyURL string
	secret    string
	jsonBody  bool // true 时按 JSON 提交（Cap Standalone 文档口径）
	http      *http.Client
}

var _ captcha.Captcha = (*Client)(nil)

// NewTurnstile 构造 Cloudflare Turnstile 客户端。
func NewTurnstile(secret string) *Client {
	return newClient("https://challenges.cloudflare.com/turnstile/v0/siteverify", secret)
}

// NewReCaptcha 构造 Google reCAPTCHA 客户端。
func NewReCaptcha(secret string) *Client {
	return newClient("https://www.google.com/recaptcha/api/siteverify", secret)
}

// NewHCaptcha 构造 hCaptcha 客户端。
func NewHCaptcha(secret string) *Client {
	return newClient("https://hcaptcha.com/siteverify", secret)
}

// NewCap 构造 Cap Standalone（自托管）客户端。
// instanceURL 为实例公网地址（如 https://cap.example.com），siteKey 为站点键，
// 验证端点为 {instanceURL}/{siteKey}/siteverify（JSON 请求体，见 cap 文档）。
func NewCap(instanceURL, siteKey, secret string) *Client {
	c := newClient(strings.TrimRight(instanceURL, "/")+"/"+siteKey+"/siteverify", secret)
	c.jsonBody = true
	return c
}

// NewWithURL 构造指向自定义 verify URL 的客户端（测试用）。
func NewWithURL(verifyURL, secret string) *Client {
	return newClient(verifyURL, secret)
}

func newClient(verifyURL, secret string) *Client {
	return &Client{verifyURL: verifyURL, secret: secret, http: &http.Client{}}
}

// Verify 校验 token。
func (c *Client) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	var (
		body        []byte
		contentType string
	)
	if c.jsonBody {
		// Cap 的 siteverify 只读 secret/response；remoteip 无对应语义，不发送。
		b, err := json.Marshal(map[string]string{"secret": c.secret, "response": token})
		if err != nil {
			return false, err
		}
		body, contentType = b, "application/json"
	} else {
		form := url.Values{}
		form.Set("secret", c.secret)
		form.Set("response", token)
		if remoteIP != "" {
			form.Set("remoteip", remoteIP)
		}
		body, contentType = []byte(form.Encode()), "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.verifyURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var v struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return false, err
	}
	return v.Success, nil
}
