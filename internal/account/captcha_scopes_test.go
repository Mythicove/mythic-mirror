package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
)

// fakeCaptcha 是测试用验证码：只有固定 token 通过，并记录调用次数。
type fakeCaptcha struct {
	okToken string
	calls   int
}

func (f *fakeCaptcha) Verify(_ context.Context, token, _ string) (bool, error) {
	f.calls++
	return token == f.okToken, nil
}

// TestCaptchaScopesGating 验证 scope 判定：默认只保护 register；login 未列入时不校验。
func TestCaptchaScopesGating(t *testing.T) {
	ctx := context.Background()
	fc := &fakeCaptcha{okToken: "good"}

	// register 在 scope 内：无 token 拒绝，好 token 放行
	svc := newTestService(yggdrasil.NewMemoryStore(), &captureMailer{})
	svc.Configure(Options{Captcha: fc, CaptchaDriver: "cap", CaptchaScopes: []string{"register"}})
	if err := svc.verifyCaptcha(ctx, "register", "", ""); err == nil {
		t.Fatal("expected captcha failure for empty token")
	}
	if err := svc.verifyCaptcha(ctx, "register", "good", ""); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	// login 不在 scope 内：直接放行且不消耗 token
	before := fc.calls
	if err := svc.verifyCaptcha(ctx, "login", "", ""); err != nil {
		t.Fatalf("login scope disabled but got %v", err)
	}
	if fc.calls != before {
		t.Fatal("verify must not be called for disabled scope")
	}
}

// TestConfigureScopeFilter 验证未知 scope 被剔除、nil 取默认、显式空列表=全关。
func TestConfigureScopeFilter(t *testing.T) {
	svc := newTestService(yggdrasil.NewMemoryStore(), &captureMailer{})
	svc.Configure(Options{CaptchaScopes: []string{"login", "bogus", " register "}})
	if len(svc.opts.CaptchaScopes) != 2 || svc.opts.CaptchaScopes[0] != "login" || svc.opts.CaptchaScopes[1] != "register" {
		t.Fatalf("scopes not filtered/trimmed: %v", svc.opts.CaptchaScopes)
	}
	svc.Configure(Options{})
	if len(svc.opts.CaptchaScopes) != 1 || svc.opts.CaptchaScopes[0] != "register" {
		t.Fatalf("nil scopes should default to register: %v", svc.opts.CaptchaScopes)
	}
	svc.Configure(Options{CaptchaScopes: []string{}})
	if len(svc.opts.CaptchaScopes) != 0 {
		t.Fatalf("explicit empty scopes must disable all: %v", svc.opts.CaptchaScopes)
	}
}

// TestLoginCaptchaEnforcement 走 HTTP 层：login scope 开启时 /account/login 强制验证码，
// 凭据错误 403，验证码错误 400，全部通过时返回会话令牌。
func TestLoginCaptchaEnforcement(t *testing.T) {
	fc := &fakeCaptcha{okToken: "good"}
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	svc.Configure(Options{Captcha: fc, CaptchaDriver: "cap", CaptchaScopes: []string{"register", "login"}})
	if _, err := svc.Register(context.Background(), RegisterRequest{
		Email: "captcha-login@example.com", Password: "pw12345678", Nickname: "A", PlayerName: "Ann", CaptchaToken: "good",
	}); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	svc.RegisterRoutes(r)

	post := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(body)))
		return rr
	}

	if rr := post(`{"email":"captcha-login@example.com","password":"pw12345678"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing captcha token: got %d want 400", rr.Code)
	}
	if rr := post(`{"email":"captcha-login@example.com","password":"pw12345678","captchaToken":"bad"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad captcha token: got %d want 400", rr.Code)
	}
	if rr := post(`{"email":"x@y.z","password":"pw12345678","captchaToken":"good"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("bad credentials: got %d want 403", rr.Code)
	}
	rr := post(`{"email":"captcha-login@example.com","password":"pw12345678","captchaToken":"good"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid login: got %d want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var resp LoginSession
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil || resp.AccessToken == "" {
		t.Fatalf("bad response: %+v err=%v", resp, err)
	}
	// 网站会话不代表任何玩家：player_id 为空（玩哪个由前端在玩家列表里选）
	sess, err := store.Validate(context.Background(), resp.AccessToken, "")
	if err != nil || sess.PlayerID != "" {
		t.Fatalf("website session must not pin a player: %+v err=%v", sess, err)
	}

	// login scope 关闭后不再要求验证码
	svc.Configure(Options{Captcha: fc, CaptchaDriver: "cap", CaptchaScopes: []string{"register"}})
	if rr := post(`{"email":"captcha-login@example.com","password":"pw12345678"}`); rr.Code != http.StatusOK {
		t.Fatalf("scope off: got %d want 200", rr.Code)
	}
}

// TestRequirementsCarryScopes 锁住 requirements 契约：验证码生效时下发 captchaScopes。
func TestRequirementsCarryScopes(t *testing.T) {
	svc := newTestService(yggdrasil.NewMemoryStore(), &captureMailer{})
	svc.Configure(Options{Captcha: &fakeCaptcha{}, CaptchaDriver: "cap", CaptchaSiteKey: "sk", CaptchaScopes: []string{"register", "login"}})
	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/register-requirements", nil))
	var body struct {
		CaptchaScopes []string `json:"captchaScopes"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.CaptchaScopes) != 2 || body.CaptchaScopes[1] != "login" {
		t.Fatalf("unexpected scopes: %v", body.CaptchaScopes)
	}

	// 验证码未生效（driver 空）→ scopes 为空数组
	svc.Configure(Options{})
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/register-requirements", nil))
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CaptchaScopes == nil || len(body.CaptchaScopes) != 0 {
		t.Fatalf("expected empty scopes, got %v", body.CaptchaScopes)
	}
}
