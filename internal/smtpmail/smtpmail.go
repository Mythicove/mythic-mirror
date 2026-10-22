// Package smtpmail 提供基于 net/smtp 的邮件实现（覆盖大多数国内外邮箱/SES SMTP 网关）。
package smtpmail

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/Mythicove/shared/mailer"
)

// Mailer 是 SMTP 邮件实现。
type Mailer struct {
	addr     string // host:port
	from     string
	username string
	password string
}

var _ mailer.Mailer = (*Mailer)(nil)

// New 构造 SMTP 邮件。
func New(addr, from, username, password string) *Mailer {
	return &Mailer{addr: addr, from: from, username: username, password: password}
}

// Send 发送邮件。
func (m *Mailer) Send(_ context.Context, msg mailer.Message) error {
	body := buildMessage(m.from, msg)
	auth := smtp.PlainAuth("", m.username, m.password, host(m.addr))
	return smtp.SendMail(m.addr, auth, m.from, []string{msg.To}, body)
}

func host(addr string) string {
	if i := strings.Index(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

func buildMessage(from string, msg mailer.Message) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + msg.To + "\r\n")
	sb.WriteString("Subject: " + msg.Subject + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(msg.Text)
	return []byte(fmt.Sprintf("%s", sb.String()))
}
