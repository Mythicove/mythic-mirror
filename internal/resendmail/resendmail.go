// Package resendmail 提供 Resend HTTP API 邮件实现（国际服务商打样）。
package resendmail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Mythicove/shared/mailer"
)

// Mailer 是 Resend 邮件实现。
type Mailer struct {
	apiKey string
	from   string
	http   *http.Client
}

var _ mailer.Mailer = (*Mailer)(nil)

// New 构造 Resend 邮件。
func New(apiKey, from string) *Mailer {
	return &Mailer{apiKey: apiKey, from: from, http: &http.Client{}}
}

// Send 发送邮件。
func (m *Mailer) Send(ctx context.Context, msg mailer.Message) error {
	payload := map[string]any{
		"from":    m.from,
		"to":      []string{msg.To},
		"subject": msg.Subject,
	}
	if msg.Text != "" {
		payload["text"] = msg.Text
	}
	if msg.HTML != "" {
		payload["html"] = msg.HTML
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resend: unexpected status %d", resp.StatusCode)
	}
	return nil
}
