// Package maillog 提供将邮件打印到日志的 Mailer（开发/低档实现）。
package maillog

import (
	"context"
	"log"

	"github.com/Mythicove/shared/mailer"
)

// Mailer 把邮件内容打印到标准日志。
type Mailer struct{}

// Send 打印邮件（不真正发送）。
func (Mailer) Send(_ context.Context, m mailer.Message) error {
	log.Printf("[mail] to=%s subject=%q\n%s\n%s", m.To, m.Subject, m.Text, m.HTML)
	return nil
}
