// Package smtpmail 提供基于 go-mail 的邮件实现。
package smtpmail

import (
	"context"
	"net"
	"strconv"

	"github.com/Mythicove/shared/mailer"
	gomail "github.com/wneessen/go-mail"
)

// Mailer 是 SMTP 邮件实现。
type Mailer struct {
	addr     string // host:port（缺端口时按 25）
	from     string
	username string
	password string
}

var _ mailer.Mailer = (*Mailer)(nil)

// New 构造 SMTP 邮件器。
func New(addr, from, username, password string) *Mailer {
	return &Mailer{addr: addr, from: from, username: username, password: password}
}

// Send 发送邮件。username 为空时不做 SMTP AUTH（mailpit/内网中继场景）。
func (m *Mailer) Send(_ context.Context, msg mailer.Message) error {
	mail := gomail.NewMsg()
	if err := mail.From(m.from); err != nil {
		return err
	}
	if err := mail.To(msg.To); err != nil {
		return err
	}
	mail.Subject(msg.Subject)
	if msg.HTML != "" {
		mail.SetBodyString(gomail.TypeTextHTML, msg.HTML)
		if msg.Text != "" {
			mail.AddAlternativeString(gomail.TypeTextPlain, msg.Text)
		}
	} else {
		mail.SetBodyString(gomail.TypeTextPlain, msg.Text)
	}
	mail.SetHeader(gomail.Header("Auto-Submitted"), "auto-generated")

	// go-mail 的 NewClient 只接受主机名；端口须单独给出。
	host, port := m.addr, 25
	if h, p, err := net.SplitHostPort(m.addr); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			host, port = h, n
		}
	}
	opts := []gomail.Option{gomail.WithPort(port)}
	switch {
	case port == 465:
		// 隐式 TLS
		opts = append(opts, gomail.WithSSL())
	case m.username != "":
		// 有凭证：STARTTLS 可用则升级
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(m.username),
			gomail.WithPassword(m.password),
			gomail.WithTLSPolicy(gomail.TLSOpportunistic),
		)
	default:
		// 无凭证：内网中继/mailpit 等明文场景（go-mail 默认强制 STARTTLS，需显式关闭）
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	c, err := gomail.NewClient(host, opts...)
	if err != nil {
		return err
	}
	return c.DialAndSend(mail)
}
