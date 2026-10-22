package account

import (
	"context"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mailer"
)

type captureMailer struct {
	msgs []mailer.Message
}

func (c *captureMailer) Send(_ context.Context, m mailer.Message) error {
	c.msgs = append(c.msgs, m)
	return nil
}

func TestRegisterVerifyReset(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	cm := &captureMailer{}
	svc := NewService(store, cm, "http://localhost")

	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "bob@example.com", Password: "password123", Nickname: "Bob", PlayerName: "Bob",
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}
	acct, err := store.AccountByEmail(ctx, "bob@example.com")
	if err != nil || !acct.Verified {
		t.Fatalf("account=%+v err=%v", acct, err)
	}

	if _, err := store.Authenticate(ctx, "bob@example.com", "password123"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	cm.msgs = nil
	if err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "bob@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, ResetPasswordRequest{
		Token: extractToken(t, cm.msgs[0].Text), Password: "newpassword1",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Authenticate(ctx, "bob@example.com", "newpassword1"); err != nil {
		t.Fatalf("authenticate after reset: %v", err)
	}
	if _, err := store.Authenticate(ctx, "bob@example.com", "password123"); err == nil {
		t.Fatal("old password should fail")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := NewService(store, &captureMailer{}, "http://localhost")

	if _, err := svc.Register(ctx, RegisterRequest{Email: "bob@example.com", Password: "password123", Nickname: "Bob", PlayerName: "Bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, RegisterRequest{Email: "bob@example.com", Password: "password123", Nickname: "Bob2", PlayerName: "Bob2"}); err == nil {
		t.Fatal("expected duplicate email error")
	}
	if _, err := svc.Register(ctx, RegisterRequest{Email: "carol@example.com", Password: "password123", Nickname: "Carol", PlayerName: "Bob"}); err == nil {
		t.Fatal("expected duplicate player name error")
	}
}

func extractToken(t *testing.T, text string) string {
	t.Helper()
	idx := strings.Index(text, "token=")
	if idx < 0 {
		t.Fatalf("no token in %q", text)
	}
	return strings.TrimSpace(text[idx+len("token="):])
}
