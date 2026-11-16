package account

import (
	"context"
	"errors"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
)

// 归一化把长度下限夹在 [MinPasswordLength, MaxPasswordLength]：0/负/过小=默认，过大=误配兜底。
func TestNormalizePasswordPolicyClamp(t *testing.T) {
	for _, tc := range []struct {
		in, want int
	}{
		{0, MinPasswordLength},
		{-5, MinPasswordLength},
		{3, MinPasswordLength},
		{12, 12},
		{100, MaxPasswordLength},
	} {
		got := NormalizeRegistrationPolicy(RegistrationPolicy{Password: PasswordPolicy{MinLength: tc.in}})
		if got.Password.MinLength != tc.want {
			t.Fatalf("min %d -> %d, want %d", tc.in, got.Password.MinLength, tc.want)
		}
	}
}

// Evaluate 逐条判定；Check 只看**已启用**的要求——未启用的不满足也不拒绝。
func TestPasswordPolicyCheck(t *testing.T) {
	pol := PasswordPolicy{MinLength: 10, RequireUpper: true, RequireDigit: true}
	if err := pol.Check("abcdefghij"); err == nil {
		t.Fatal("missing upper+digit should fail")
	}
	if err := pol.Check("Abcdefghij"); err == nil {
		t.Fatal("missing digit should fail")
	}
	if err := pol.Check("Abcdefgh1"); err == nil {
		t.Fatal("too short should fail")
	}
	if err := pol.Check("Abcdefghi1"); err != nil {
		t.Fatalf("conforming password rejected: %v", err)
	}

	sym := PasswordPolicy{MinLength: 8, RequireSymbol: true}
	if err := sym.Check("abcdefg1"); err == nil {
		t.Fatal("missing symbol should fail")
	}
	if err := sym.Check("abcdefg1!"); err != nil {
		t.Fatalf("symbol present rejected: %v", err)
	}

	// Evaluate 对未启用项也给出判定，供前端灰显清单
	c := sym.Evaluate("ABCdefg1")
	if !c.Upper || !c.Lower || !c.Digit || !c.Length || c.Symbol {
		t.Fatalf("evaluate flags wrong: %+v", c)
	}
}

// 注册/重置/启动器凭证三处都走后台策略，而非写死长度常量。
func TestComplexityEnforcedAtAllPasswordSites(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	cm := &captureMailer{}
	svc := newTestService(store, cm)
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{
		Password: PasswordPolicy{MinLength: 10, RequireDigit: true},
	}})

	// 注册：无数字拒绝、合规放行
	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "a@example.com", Password: "abcdefghij", PlayerName: "ComplexOne",
	}); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("register without digit: %v", err)
	}
	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "a@example.com", Password: "abcdefghi1", PlayerName: "ComplexOne",
	}); err != nil {
		t.Fatalf("conforming register rejected: %v", err)
	}
	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}

	// 收紧策略后：重置与启动器凭证同样受新策略约束
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{
		Password: PasswordPolicy{MinLength: 10, RequireDigit: true, RequireSymbol: true},
	}})

	cm.msgs = nil
	if err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "a@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, ResetPasswordRequest{
		Token: extractToken(t, cm.msgs[0].Text), Password: "abcdefghi1",
	}); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("reset without symbol under new policy: %v", err)
	}

	acct, err := store.AccountByEmail(ctx, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sctx := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: acct.ID})
	if _, err := svc.CreatePlayerCredential(sctx, "ComplexOne", "desktop", "abcdefghi1"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("launcher credential without symbol: %v", err)
	}
	if _, err := svc.CreatePlayerCredential(sctx, "ComplexOne", "desktop", "abcdefghi1!"); err != nil {
		t.Fatalf("conforming credential rejected: %v", err)
	}
}
