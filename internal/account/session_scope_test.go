package account

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mythicove/shared/mc"
	"github.com/go-chi/chi/v5"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
)

// 种子账号 seed-admin 是 role=admin、有玩家 Alice 与一条独立密码 seed-cred——
// 正好同时覆盖两件事：跨玩家越权，以及"管理员账号的启动器令牌不是管理员令牌"。

func launcherCtx(ctx context.Context, playerID string) context.Context {
	return yggdrasil.WithSession(ctx, &yggdrasil.Session{
		UserID: "seed-admin", PlayerID: playerID, CredentialID: "seed-cred",
	})
}

func webCtx(ctx context.Context) context.Context {
	return yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: "seed-admin"})
}

// 一条独立密码只证明它所属玩家的身份（ADR-0012）。这条测试盯的是它的反面：
// 授权若一律按 UserID 走，拿到 Alice 的启动器令牌就等于拿到整个账号。
func TestLauncherSessionIsScopedToItsPlayer(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	svc.SetPolicy(stubPolicy{p: yggdrasil.AuthPolicy{PasswordLogin: true, MaxPlayers: 3}})

	alice := mc.UUIDFromName("Alice")
	web := webCtx(ctx)
	launcher := launcherCtx(ctx, alice)

	if _, err := svc.AddPlayer(web, "Bob"); err != nil {
		t.Fatal(err)
	}

	// 网站会话看到账号下全部玩家；启动器会话只看到它钉住的那一个
	if all, err := svc.scopedPlayers(web); err != nil || len(all) != 2 {
		t.Fatalf("web session should see every player: %+v err=%v", all, err)
	}
	only, err := svc.scopedPlayers(launcher)
	if err != nil || len(only) != 1 || only[0].Name != "Alice" {
		t.Fatalf("launcher session must see only its own player: %+v err=%v", only, err)
	}

	// 自己的玩家：查、建、删都通
	if _, err := svc.PlayerCredentials(launcher, "Alice"); err != nil {
		t.Fatalf("own credentials: %v", err)
	}
	created, err := svc.CreatePlayerCredential(launcher, "Alice", "desktop", "launcher-pw")
	if err != nil {
		t.Fatalf("create own credential: %v", err)
	}
	if err := svc.DeletePlayerCredential(launcher, "Alice", created.ID); err != nil {
		t.Fatalf("delete own credential: %v", err)
	}

	// 同账号下别的玩家：一律 NotFound，且不区分"不存在"与"你无权"——
	// 否则可以用玩家名探测账号结构。
	if _, err := svc.PlayerCredentials(launcher, "Bob"); !errors.Is(err, yggdrasil.ErrNotFound) {
		t.Fatalf("sibling credentials must be NotFound: %v", err)
	}
	// 这一条是最要紧的：它把"知道 Alice 的独立密码"变成"控制 Bob"。
	if _, err := svc.CreatePlayerCredential(launcher, "Bob", "stolen", "launcher-pw"); !errors.Is(err, yggdrasil.ErrNotFound) {
		t.Fatalf("must not mint a credential for a sibling player: %v", err)
	}
	if err := svc.DeletePlayerCredential(launcher, "Bob", "whatever"); !errors.Is(err, yggdrasil.ErrNotFound) {
		t.Fatalf("must not revoke a sibling's credential: %v", err)
	}
	// 网站会话仍能管 Bob（证明上面三条不是"这个端点坏了"）
	if _, err := svc.CreatePlayerCredential(web, "Bob", "laptop", "launcher-pw"); err != nil {
		t.Fatalf("web session should still manage any own player: %v", err)
	}
}

// 路由级：玩家级端点收启动器令牌，账号级端点回稳定码 web_session_required。
// 分开断言是因为这两组共用 Auth，只有中间件链不同——漏挂一层，服务级测试全绿而
// HTTP 上照样越权。
func TestAccountRoutesSplitPlayerAndAccountScope(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})

	launcherToken, err := store.CreateSession(ctx, "seed-admin", mc.UUIDFromName("Alice"), "seed-cred", "ct")
	if err != nil {
		t.Fatal(err)
	}
	webToken, err := store.CreateSession(ctx, "seed-admin", "", "", "ct")
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rd)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	// 玩家级：放行，且列表已被裁到钉住的那一个
	rr := do(http.MethodGet, "/account/players", launcherToken, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("launcher GET /account/players: %d %s", rr.Code, rr.Body.String())
	}
	var players []yggdrasil.Player
	if err := json.Unmarshal(rr.Body.Bytes(), &players); err != nil {
		t.Fatalf("players body %q: %v", rr.Body.String(), err)
	}
	if len(players) != 1 || players[0].Name != "Alice" {
		t.Fatalf("launcher should only see its own player, got %+v", players)
	}
	if rr := do(http.MethodGet, "/account/players/Alice/credentials", launcherToken, ""); rr.Code != http.StatusOK {
		t.Fatalf("launcher reads own credentials: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPost, "/account/players/Alice/credentials", launcherToken,
		`{"name":"desktop","password":"launcher-pw"}`); rr.Code != http.StatusCreated {
		t.Fatalf("launcher creates own credential: %d %s", rr.Code, rr.Body.String())
	}
	// 路径参数换成别的玩家名 → 404（授权在服务层，不在路由上）
	if rr := do(http.MethodGet, "/account/players/Bob/credentials", launcherToken, ""); rr.Code != http.StatusNotFound {
		t.Fatalf("launcher reads a sibling's credentials: %d %s", rr.Code, rr.Body.String())
	}
	// Publishing also selects a player; it must use the same scope as credentials.
	if _, err := store.AddPlayer(ctx, "seed-admin", "Bob", 3); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Alice", "Bob"} {
		if err := store.SetSkin(ctx, mc.UUIDFromName(name), "published.png", "default"); err != nil {
			t.Fatal(err)
		}
	}
	if rr := do(http.MethodPost, "/skinlib", launcherToken, `{"playerName":"Bob","description":"sibling"}`); rr.Code != http.StatusNotFound {
		t.Fatalf("launcher must not publish a sibling's skin: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPost, "/skinlib", launcherToken, `{"playerName":"Alice","description":"own"}`); rr.Code != http.StatusCreated {
		t.Fatalf("launcher publishes its own skin: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPost, "/skinlib", webToken, `{"playerName":"Bob","description":"web"}`); rr.Code != http.StatusCreated {
		t.Fatalf("web session publishes an owned sibling's skin: %d %s", rr.Code, rr.Body.String())
	}

	// 账号级：一律 403 + 稳定码
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/account/players", `{"name":"Mallory"}`},
		{http.MethodGet, "/account/me", ""},
		{http.MethodGet, "/account/identities", ""},
		{http.MethodDelete, "/account/players/Alice", ""},
		{http.MethodPatch, "/account/players/Alice", `{"name":"Renamed"}`},
		{http.MethodPost, "/account/password", `{"currentPassword":"password","newPassword":"another-pw"}`},
	} {
		rr := do(c.method, c.path, launcherToken, c.body)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s %s with a launcher token: want 403, got %d %s", c.method, c.path, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), yggdrasil.WebSessionRequiredCode) {
			t.Fatalf("%s %s: body should carry the stable code, got %q", c.method, c.path, rr.Body.String())
		}
	}

	// 网站令牌不受影响（否则这次收紧只是把功能关掉）
	if rr := do(http.MethodGet, "/account/me", webToken, ""); rr.Code != http.StatusOK {
		t.Fatalf("web GET /account/me: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodGet, "/account/identities", webToken, ""); rr.Code != http.StatusOK {
		t.Fatalf("web GET /account/identities: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodPost, "/account/players", webToken, `{"name":"Carol"}`); rr.Code != http.StatusOK &&
		rr.Code != http.StatusCreated {
		t.Fatalf("web POST /account/players: %d %s", rr.Code, rr.Body.String())
	}
}
