package yggdrasil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/shared/mc"
	"github.com/go-chi/chi/v5"
)

// stubPolicy 是测试用的策略来源（生产由 admin.Service 提供）。
type stubPolicy struct{ p AuthPolicy }

func (s stubPolicy) AuthPolicy(context.Context) (AuthPolicy, error) { return s.p, nil }

// newPolicyServer 起一个带指定策略的 Yggdrasil HTTP 服务（内存存储）。
func newPolicyServer(t *testing.T, policy AuthPolicy) (*httptest.Server, *MemoryStore) {
	t.Helper()
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	svc := NewService(store, kp, fs, ServiceOptions{
		ServerName: "test", ImplementationName: "test", SkinDomains: []string{"localhost"},
	})
	svc.SetPolicy(stubPolicy{p: policy})
	r := chi.NewRouter()
	svc.Register(r)
	return httptest.NewServer(r), store
}

// 启动器登录只认「玩家名 + 该玩家的独立密码」（ADR-0012）：
// 主密码、邮箱、以及没有独立密码的玩家都进不来；会话钉住认证到的那个玩家与那条凭证。
func TestLauncherAuthenticate(t *testing.T) {
	ctx := context.Background()
	srv, store := newPolicyServer(t, AuthPolicy{PasswordLogin: true})
	defer srv.Close()

	body := `{"username":"Alice","password":"password"}`
	resp, err := http.Post(srv.URL+"/authserver/authenticate", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("launcher login: %d", resp.StatusCode)
	}
	var auth AuthenticateResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		t.Fatal(err)
	}
	if auth.SelectedProfile == nil || auth.SelectedProfile.Name != "Alice" {
		t.Fatalf("selected profile: %+v", auth.SelectedProfile)
	}
	// 一条独立密码只证明这一个玩家：同账号下别的玩家不被列出
	if len(auth.AvailableProfiles) != 1 || auth.AvailableProfiles[0].Name != "Alice" {
		t.Fatalf("available profiles: %+v", auth.AvailableProfiles)
	}
	sess, err := store.Validate(ctx, auth.AccessToken, "")
	if err != nil || sess.PlayerID != mc.UUIDFromName("Alice") || sess.CredentialID == "" {
		t.Fatalf("session must pin the player and the credential: %+v err=%v", sess, err)
	}

	// 邮箱 + 主密码不再能登启动器（种子账号的主密码同样是 "password"，用户名不同而已）
	post := func(username, password string) int {
		r, err := http.Post(srv.URL+"/authserver/authenticate", "application/json",
			bytes.NewReader([]byte(`{"username":"`+username+`","password":"`+password+`"}`)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}
	if code := post("admin@example.com", "password"); code != http.StatusForbidden {
		t.Fatalf("email + master password must not reach the launcher: %d", code)
	}
	// 新玩家没有独立密码 → 即使密码与主密码相同也进不来
	if _, err := store.AddPlayer(ctx, "seed-admin", "Bob", 0); err != nil {
		t.Fatal(err)
	}
	if code := post("Bob", "password"); code != http.StatusForbidden {
		t.Fatalf("player without a launcher credential must be rejected: %d", code)
	}
	// 封禁账号即使有独立密码也不得进服
	if err := store.UpdateUser(ctx, "seed-admin", "admin", "banned"); err != nil {
		t.Fatal(err)
	}
	if code := post("Alice", "password"); code != http.StatusForbidden {
		t.Fatalf("banned account must not enter a server: %d", code)
	}
}

// 强制绑定第三方时：未绑定账号的启动器登录与皮肤上传都被拒，绑定后放行。
// 两处共用 CheckBinding 这一条规则，错误码稳定，前端据此弹绑定引导。
func TestRequireBinding(t *testing.T) {
	ctx := context.Background()
	srv, store := newPolicyServer(t, AuthPolicy{PasswordLogin: true, RequireBinding: true})
	defer srv.Close()

	if _, err := store.AuthenticatePlayer(ctx, "Alice", "password"); err != nil {
		t.Fatalf("store-level auth should not know about the policy: %v", err)
	}
	resp, err := http.Post(srv.URL+"/authserver/authenticate", "application/json",
		bytes.NewReader([]byte(`{"username":"Alice","password":"password"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unbound launcher login: %d", resp.StatusCode)
	}

	// 网站会话（不经启动器登录）同样受皮肤上传的门禁
	tok, err := store.CreateSession(ctx, "seed-admin", "", "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	upload := func() (int, string) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/skin/Alice", bytes.NewReader([]byte("not-a-png")))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		return r.StatusCode, body.Error
	}
	if code, errCode := upload(); code != http.StatusForbidden || errCode != BindingRequiredCode {
		t.Fatalf("unbound upload: %d %q", code, errCode)
	}

	if err := store.UpsertIdentity(ctx, "github", "gh-1", "seed-admin", "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	if code, errCode := upload(); code == http.StatusForbidden && errCode == BindingRequiredCode {
		t.Fatalf("bound account must pass the gate: %d %q", code, errCode)
	}
	resp2, err := http.Post(srv.URL+"/authserver/authenticate", "application/json",
		bytes.NewReader([]byte(`{"username":"Alice","password":"password"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("bound launcher login: %d", resp2.StatusCode)
	}
}

// 撤销独立密码 → 由它建立的启动器会话立即失效（不是等 24 小时过期）。
func TestRevokeCredentialKillsSession(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	auth, err := store.AuthenticatePlayer(ctx, "Alice", "password")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := store.CreateSession(ctx, auth.UserID, auth.Player.ID, auth.CredentialID, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(ctx, tok, ""); err != nil {
		t.Fatalf("session should validate: %v", err)
	}
	if err := store.DeletePlayerCredential(ctx, auth.Player.ID, auth.CredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Validate(ctx, tok, ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked credential must kill its sessions: %v", err)
	}
	if _, err := store.AuthenticatePlayer(ctx, "Alice", "password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("revoked credential must not authenticate: %v", err)
	}
}
