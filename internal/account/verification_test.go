package account

import (
	"context"
	"errors"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
)

// 未验证账号可被同邮箱重注册接管（覆盖密码+重发验证）；已验证账号拒绝接管。
func TestReregisterTakesOverUnverified(t *testing.T) {
	ctx := context.Background()
	cm := &captureMailer{}
	svc := newTestService(yggdrasil.NewMemoryStore(), cm)

	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "v2@example.com", Password: "password123", PlayerName: "TakeOver",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "v2@example.com", Password: "newpassword1", PlayerName: "TakeOver",
	}); err != nil {
		t.Fatalf("takeover of unverified account must succeed: %v", err)
	}
	// 旧密码作废
	if _, err := svc.Login(ctx, LoginRequest{Email: "v2@example.com", Password: "password123"}); err == nil {
		t.Fatal("old password must be invalid after takeover")
	}
	// 用接管后的新验证邮件完成验证
	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[len(cm.msgs)-1].Text)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Email: "v2@example.com", Password: "newpassword1"}); err != nil {
		t.Fatalf("login after verify: %v", err)
	}
	// 已验证账号不得被接管：重复注册被明确拒绝（email_registered / name_taken 都算拒绝）
	_, err := svc.Register(ctx, RegisterRequest{
		Email: "v2@example.com", Password: "anotherpass1", PlayerName: "TakeOver",
	})
	if !errors.Is(err, ErrEmailRegistered) &&
		!errors.Is(err, yggdrasil.ErrAlreadyExists) &&
		!errors.Is(err, ErrNameTaken) {
		t.Fatalf("verified account must not be taken over, got %v", err)
	}
}

// 默认策略要求邮件验证：未验证拿不到会话；重发只给"存在且未验证"的账号发信；验证后放行。
func TestLoginGatedUntilVerified(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	cm := &captureMailer{}
	svc := newTestService(store, cm)

	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "v@example.com", Password: "password123", PlayerName: "VerifyMe",
	}); err != nil {
		t.Fatal(err)
	}
	if len(cm.msgs) != 1 {
		t.Fatalf("register should send one verify mail, got %d", len(cm.msgs))
	}

	if _, err := svc.Login(ctx, LoginRequest{Email: "v@example.com", Password: "password123"}); !errors.Is(err, yggdrasil.ErrEmailNotVerified) {
		t.Fatalf("unverified login must be refused with ErrEmailNotVerified, got %v", err)
	}

	// 重发：未验证账号追加一封；不存在的邮箱静默不发
	if err := svc.ResendVerification(ctx, "v@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(cm.msgs) != 2 {
		t.Fatalf("resend should add a mail, got %d", len(cm.msgs))
	}
	if err := svc.ResendVerification(ctx, "ghost@example.com"); err != nil {
		t.Fatalf("unknown email must be silent, got %v", err)
	}
	if len(cm.msgs) != 2 {
		t.Fatalf("unknown email must not send, got %d", len(cm.msgs))
	}

	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Email: "v@example.com", Password: "password123"}); err != nil {
		t.Fatalf("verified login must pass: %v", err)
	}
	// 已验证再重发不再发信
	if err := svc.ResendVerification(ctx, "v@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(cm.msgs) != 2 {
		t.Fatalf("verified resend must not send, got %d", len(cm.msgs))
	}
}

// 后台放行开关：AllowUnverifiedLogin=true 时未验证也能登录（救援/迁移场景）。
func TestAllowUnverifiedLoginBypass(t *testing.T) {
	ctx := context.Background()
	cm := &captureMailer{}
	svc := newTestService(yggdrasil.NewMemoryStore(), cm)
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{AllowUnverifiedLogin: true}})

	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "u@example.com", Password: "password123", PlayerName: "Unverified",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Email: "u@example.com", Password: "password123"}); err != nil {
		t.Fatalf("allowUnverifiedLogin must let unverified login through: %v", err)
	}
}
