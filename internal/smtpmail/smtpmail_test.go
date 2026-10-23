package smtpmail

import (
	"context"
	"testing"

	"github.com/Mythicove/shared/mailer"
)

// 不可达地址应快速返回错误（同时覆盖 host:port 解析路径）。
func TestSendUnreachable(t *testing.T) {
	m := New("127.0.0.1:1", "sender@example.com", "", "")
	err := m.Send(context.Background(), mailer.Message{To: "to@example.com", Subject: "s", Text: "t"})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestSendInvalidAddress(t *testing.T) {
	m := New("smtp://bad addr:xx", "not-an-email", "", "")
	if err := m.Send(context.Background(), mailer.Message{To: "to@example.com"}); err == nil {
		t.Fatal("expected from-validation error")
	}
}
