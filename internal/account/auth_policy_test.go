package account

import (
	"context"
	"errors"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/oauth"
)

// stubPolicy 是测试用的策略来源（生产由 admin.Service 提供）。
type stubPolicy struct{ p yggdrasil.AuthPolicy }

func (s stubPolicy) AuthPolicy(context.Context) (yggdrasil.AuthPolicy, error) { return s.p, nil }

// registerVerified 注册并走完邮箱验证（未验证的账号不计入"纯密码账号"）。
func registerVerified(t *testing.T, svc *Service, cm *captureMailer, email, password, player string) string {
	t.Helper()
	ctx := context.Background()
	cm.msgs = nil
	if _, err := svc.Register(ctx, RegisterRequest{
		Email: email, Password: password, Nickname: player, PlayerName: player,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}
	acct, err := svc.store.AccountByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	return acct.ID
}

// 主密码关闭后的降级路径：未绑定第三方的历史账号仍可登录/重置（宽限），
// 已绑定的账号一律拒绝，账号密码注册立即关闭。于是纯密码账号归零的那一刻
// 主密码登录自然完全失效——不需要第二个开关，也不需要在登录热路径上统计全站人数。
func TestPasswordLoginPolicyGrace(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	cm := &captureMailer{}
	svc := newTestService(store, cm)
	svc.Configure(Options{OAuthProviders: map[string]oauth.Provider{
		"github": fakeProvider{profile: oauth.Profile{
			Provider: "github", UID: "gh-1", Email: "bound@example.com", Nickname: "Bound",
		}},
	}})

	legacyID := registerVerified(t, svc, cm, "legacy@example.com", "password123", "Legacy")
	registerVerified(t, svc, cm, "bound@example.com", "password123", "BoundOne")

	// 第三方登录命中同邮箱的既有账号 → 写下绑定（此前只按 email 匹配、不留痕）
	if _, err := svc.OAuthLogin(ctx, "github", "good"); err != nil {
		t.Fatal(err)
	}
	boundAcct, err := store.AccountByEmail(ctx, "bound@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.IdentityUser(ctx, "github", "gh-1"); err != nil || got != boundAcct.ID {
		t.Fatalf("oauth login must record a binding: %q err=%v", got, err)
	}

	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: false, MaxPlayers: 1}})

	if _, err := svc.Login(ctx, LoginRequest{Email: "legacy@example.com", Password: "password123"}); err != nil {
		t.Fatalf("unbound legacy account keeps password login (grace): %v", err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Email: "bound@example.com", Password: "password123"}); !errors.Is(err, yggdrasil.ErrPasswordLoginDisabled) {
		t.Fatalf("bound account must be rejected once password login is off: %v", err)
	}
	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "new@example.com", Password: "password123", Nickname: "N", PlayerName: "Newbie",
	}); !errors.Is(err, yggdrasil.ErrPasswordLoginDisabled) {
		t.Fatalf("password signup must close with password login: %v", err)
	}

	// 重置走同一条策略：已绑定拒绝，未绑定仍可用（否则历史账号被彻底锁死）
	cm.msgs = nil
	if err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "bound@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, ResetPasswordRequest{
		Token: extractToken(t, cm.msgs[0].Text), Password: "newpassword1",
	}); !errors.Is(err, yggdrasil.ErrPasswordLoginDisabled) {
		t.Fatalf("reset must follow the same policy: %v", err)
	}
	cm.msgs = nil
	if err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Email: "legacy@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetPassword(ctx, ResetPasswordRequest{
		Token: extractToken(t, cm.msgs[0].Text), Password: "newpassword1",
	}); err != nil {
		t.Fatalf("legacy reset should still work: %v", err)
	}

	// 计数只服务管理面板的决策提示：种子 admin + legacy 都是纯密码账号，bound 已绑定不计
	before, err := store.CountPasswordOnlyUsers(ctx)
	if err != nil || before != 2 {
		t.Fatalf("password-only count: %d err=%v", before, err)
	}
	// legacy 绑定第三方后宽限消失：登录被拒，计数减一
	if err := store.UpsertIdentity(ctx, "github", "gh-legacy", legacyID, "legacy@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Email: "legacy@example.com", Password: "newpassword1"}); !errors.Is(err, yggdrasil.ErrPasswordLoginDisabled) {
		t.Fatalf("grace ends as soon as the account is bound: %v", err)
	}
	if after, err := store.CountPasswordOnlyUsers(ctx); err != nil || after != before-1 {
		t.Fatalf("count after binding: %d (was %d) err=%v", after, before, err)
	}

	// 策略打开时一切照旧
	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: true, MaxPlayers: 1}})
	if _, err := svc.Login(ctx, LoginRequest{Email: "legacy@example.com", Password: "newpassword1"}); err != nil {
		t.Fatalf("password login restored: %v", err)
	}
}

// 玩家数上限来自策略（默认 1）；独立密码每玩家至多 MaxCredentialsPerPlayer 条。
func TestPlayerAndCredentialLimits(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: true, MaxPlayers: 1}})

	const uid = "seed-admin" // 种子账号已有一个玩家 Alice
	sctx := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: uid})
	if _, err := svc.AddPlayer(sctx, "Second"); !errors.Is(err, yggdrasil.ErrPlayerLimit) {
		t.Fatalf("player limit: %v", err)
	}
	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: true, MaxPlayers: 2}})
	if _, err := svc.AddPlayer(sctx, "Second"); err != nil {
		t.Fatalf("raise the limit and it should pass: %v", err)
	}

	// 种子里 Alice 已带一条独立密码；先撤销它，好让下面的上限断言从 0 开始
	seed, err := svc.PlayerCredentials(sctx, "Alice")
	if err != nil || len(seed) != 1 {
		t.Fatalf("seed credentials: %+v err=%v", seed, err)
	}
	if err := svc.DeletePlayerCredential(sctx, "Alice", seed[0].ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxCredentialsPerPlayer; i++ {
		if _, err := svc.CreatePlayerCredential(sctx, "Alice", "device", "launcher-pw"); err != nil {
			t.Fatalf("credential %d: %v", i, err)
		}
	}
	created, err := svc.CreatePlayerCredential(sctx, "Alice", "one-too-many", "launcher-pw")
	if !errors.Is(err, yggdrasil.ErrCredentialLimit) {
		t.Fatalf("credential cap: %+v err=%v", created, err)
	}
	// 标签与密码长度都要校验
	if _, err := svc.CreatePlayerCredential(sctx, "Alice", "  ", "launcher-pw"); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("blank label: %v", err)
	}
	if _, err := svc.CreatePlayerCredential(sctx, "Alice", "x", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak launcher password: %v", err)
	}
	// 撤销一条后又有位置；别人的玩家一律 NotFound
	list, err := svc.PlayerCredentials(sctx, "Alice")
	if err != nil || len(list) != MaxCredentialsPerPlayer {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	if err := svc.DeletePlayerCredential(sctx, "Alice", list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePlayerCredential(sctx, "Alice", "device", "launcher-pw"); err != nil {
		t.Fatalf("slot freed after revoke: %v", err)
	}
	other := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: "someone-else"})
	if _, err := svc.PlayerCredentials(other, "Alice"); !errors.Is(err, yggdrasil.ErrNotFound) {
		t.Fatalf("foreign player credentials: %v", err)
	}
}

// Identities 把绑定列表与"站点是否强制绑定"一起给出，前端据此决定要不要弹引导。
func TestIdentitiesView(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	const uid = "seed-admin"
	sctx := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: uid})

	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: true, RequireBinding: true}})
	view, err := svc.Identities(sctx)
	if err != nil || view.Required != true || len(view.Identities) != 0 {
		t.Fatalf("unbound view: %+v err=%v", view, err)
	}
	if err := store.UpsertIdentity(ctx, "github", "gh-1", uid, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Identities(sctx)
	if err != nil || len(view.Identities) != 1 || view.Identities[0].Provider != "github" {
		t.Fatalf("bound view: %+v err=%v", view, err)
	}
	if _, err := svc.Identities(ctx); !errors.Is(err, yggdrasil.ErrInvalidToken) {
		t.Fatalf("identities require a session: %v", err)
	}
}
