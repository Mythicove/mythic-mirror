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

// stubRegPolicy 是测试用的注册策略来源（生产由 admin.Service 提供）。
type stubRegPolicy struct{ p RegistrationPolicy }

func (s stubRegPolicy) RegistrationPolicy(context.Context) (RegistrationPolicy, error) { return s.p, nil }

// TestCaptchaScopesGating 验证 scope 判定：默认只保护 register；login 未列入时不校验。
func TestCaptchaScopesGating(t *testing.T) {
	ctx := context.Background()
	fc := &fakeCaptcha{okToken: "good"}

	svc := newTestService(yggdrasil.NewMemoryStore(), &captureMailer{})
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{CaptchaScopes: []string{"register"}}})
	svc.Configure(Options{Captcha: fc, CaptchaDriver: "cap"})
	if err := svc.verifyCaptcha(ctx, "register", "", ""); err == nil {
		t.Fatal("expected captcha failure for empty token")
	}
	if err := svc.verifyCaptcha(ctx, "register", "good", ""); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	before := fc.calls
	if err := svc.verifyCaptcha(ctx, "login", "", ""); err != nil {
		t.Fatalf("login scope disabled but got %v", err)
	}
	if fc.calls != before {
		t.Fatal("verify must not be called for disabled scope")
	}
}

// TestNormalizeRegistrationPolicy 验证归一化：未知 scope 剔除、nil=默认、显式空=全关。
func TestNormalizeRegistrationPolicy(t *testing.T) {
	got := NormalizeRegistrationPolicy(RegistrationPolicy{CaptchaScopes: []string{"login", "bogus", " register "}})
	if len(got.CaptchaScopes) != 2 || got.CaptchaScopes[0] != "login" || got.CaptchaScopes[1] != "register" {
		t.Fatalf("scopes not filtered/trimmed: %v", got.CaptchaScopes)
	}
	got = NormalizeRegistrationPolicy(RegistrationPolicy{})
	if len(got.CaptchaScopes) != 1 || got.CaptchaScopes[0] != "register" {
		t.Fatalf("nil scopes should default to register: %v", got.CaptchaScopes)
	}
	got = NormalizeRegistrationPolicy(RegistrationPolicy{CaptchaScopes: []string{}})
	if len(got.CaptchaScopes) != 0 {
		t.Fatalf("explicit empty scopes must disable all: %v", got.CaptchaScopes)
	}
	if got.EmailDomainWhitelist == nil {
		t.Fatal("whitelist nil should normalize to empty slice")
	}
}

// TestLoginCaptchaEnforcement 走 HTTP 层：login scope 开启时 /account/login 强制验证码，
// 凭据错误 403，验证码错误 400，全部通过时返回会话；scope 关闭后不再要求。
func TestLoginCaptchaEnforcement(t *testing.T) {
	fc := &fakeCaptcha{okToken: "good"}
	cm := &captureMailer{}
	svc := newTestService(yggdrasil.NewMemoryStore(), cm)
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{CaptchaScopes: []string{"register", "login"}}})
	svc.Configure(Options{Captcha: fc, CaptchaDriver: "cap"})

	if _, err := svc.Register(context.Background(), RegisterRequest{
		Email: "a@example.com", Password: "password123", Nickname: "Carol", PlayerName: "Carol", CaptchaToken: "good",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(context.Background(), extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	post := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(body)))
		return rr
	}

	if rr := post(`{"email":"a@example.com","password":"password123"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing captcha token: got %d want 400", rr.Code)
	}
	if rr := post(`{"email":"a@example.com","password":"password123","captchaToken":"bad"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad captcha token: got %d want 400", rr.Code)
	}
	if rr := post(`{"email":"a@example.com","password":"wrongpass","captchaToken":"good"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("bad credentials: got %d want 403", rr.Code)
	}
	if rr := post(`{"email":"a@example.com","password":"password123","captchaToken":"good"}`); rr.Code != http.StatusOK {
		t.Fatalf("valid login: got %d want 200 (body %s)", rr.Code, rr.Body.String())
	}

	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{CaptchaScopes: []string{"register"}}})
	if rr := post(`{"email":"a@example.com","password":"password123"}`); rr.Code != http.StatusOK {
		t.Fatalf("scope off: got %d want 200", rr.Code)
	}
}

// TestRequirementsCarryScopes 锁住 requirements 契约：开关/白名单/scopes 全部来自后台策略。
func TestRequirementsCarryScopes(t *testing.T) {
	svc := newTestService(yggdrasil.NewMemoryStore(), &captureMailer{})
	svc.SetPolicySource(stubRegPolicy{RegistrationPolicy{
		CaptchaScopes:        []string{"register", "login"},
		EmailDomainWhitelist: []string{"example.com"},
		InviteOnly:           true,
	}})
	svc.Configure(Options{Captcha: &fakeCaptcha{}, CaptchaDriver: "cap", CaptchaSiteKey: "sk"})
	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/register-requirements", nil))
	var body struct {
		CaptchaScopes []string `json:"captchaScopes"`
		InviteOnly    bool     `json:"inviteOnly"`
		EmailDomains  []string `json:"emailDomains"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.CaptchaScopes) != 2 || body.CaptchaScopes[1] != "login" || !body.InviteOnly || body.EmailDomains[0] != "example.com" {
		t.Fatalf("unexpected requirements: %+v", body)
	}

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
