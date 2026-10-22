// Package smtpmail 提供基于 go-mail 的邮件实现。
package smtpmail

import (
	"context"

	"github.com/Mythicove/shared/mailer"
	gomail "github.com/wneessen/go-mail"
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
	mail := gomail.NewMsg()
	if err := mail.From(m.from); err != nil {
		return err
	}
	if err := mail.To(msg.To); err != nil {
		return err
	}
	mail.Subject(msg.Subject)
	mail.SetBodyString(gomail.TypeTextPlain, msg.Text)
	c, err := gomail.NewClient(m.addr,
		gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
		gomail.WithUsername(m.username),
		gomail.WithPassword(m.password),
	)
	if err != nil {
		return err
	}
	return c.DialAndSend(mail)
}
