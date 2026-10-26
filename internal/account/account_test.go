package account

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/i18n"
	"github.com/Mythicove/mythic-mirror/internal/memcache"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mailer"
	"github.com/go-chi/chi/v5"
)

func newTestService(store *yggdrasil.MemoryStore, m mailer.Mailer) *Service {
	svc := NewService(store, m, "http://localhost", memcache.New())
	loc, err := i18n.New()
	if err != nil {
		panic(err)
	}
	svc.SetLocalizer(loc, "en-US")
	return svc
}

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
	svc := newTestService(store, cm)

	if _, err := svc.Register(ctx, RegisterRequest{
		Email: "bob@example.com", Password: "password123", Nickname: "Bob", PlayerName: "Bob",
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.VerifyEmail(ctx, extractToken(t, cm.msgs[0].Text)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cm.msgs[0].HTML, "<a href=") || !strings.Contains(cm.msgs[0].HTML, "token=") {
		t.Fatalf("expected HTML mail with link, got %q", cm.msgs[0].HTML)
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
	svc := newTestService(store, &captureMailer{})

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
	rest := text[idx+len("token="):]
	if end := strings.IndexAny(rest, " \t\r\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// TestRegisterRequirementsCapURL 锁住 /api/register-requirements 对 cap driver 的契约：
// captchaCapUrl 必须随 driver/siteKey 一并下发（前端加载 cap 控件依赖它）。
func TestRegisterRequirementsCapURL(t *testing.T) {
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	svc.Configure(Options{
		CaptchaDriver:  "cap",
		CaptchaSiteKey: "d9256640cb53",
		CaptchaCapURL:  "https://cap.example.com",
	})
	r := chi.NewRouter()
	svc.RegisterRoutes(r)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/register-requirements", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["captcha"] != "cap" || body["captchaSiteKey"] != "d9256640cb53" || body["captchaCapUrl"] != "https://cap.example.com" {
		t.Fatalf("unexpected body: %v", body)
	}
}
