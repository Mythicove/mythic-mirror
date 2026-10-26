package account

import (
	"context"
	"errors"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/oauth"
)

// fakeProvider 是测试用的第三方登录源。
type fakeProvider struct{ profile oauth.Profile }

func (f fakeProvider) AuthURL(state string) string { return "https://idp.test/auth?state=" + state }

func (f fakeProvider) Exchange(_ context.Context, code string) (*oauth.Profile, error) {
	if code != "good" {
		return nil, errors.New("bad code")
	}
	p := f.profile
	return &p, nil
}

func newOAuthTestService(t *testing.T) (*Service, yggdrasil.Store) {
	t.Helper()
	store := yggdrasil.NewMemoryStore()
	svc := NewService(store, nil, "http://mirror.test", nil)
	svc.Configure(Options{
		OAuthProviders: map[string]oauth.Provider{
			"github": fakeProvider{profile: oauth.Profile{
				Provider: "github", UID: "gh-1", Email: "gh@example.com", Nickname: "GhUser",
			}},
		},
	})
	return svc, store
}

// 第三方登录回跳不再把会话令牌交给浏览器：改为一次性换取码，由 web 服务端兑换。
func TestOAuthLoginCodeIsSingleUse(t *testing.T) {
	ctx := context.Background()
	svc, store := newOAuthTestService(t)

	code, err := svc.OAuthLoginCode(ctx, "github", "good")
	if err != nil || code == "" {
		t.Fatalf("OAuthLoginCode: %q err=%v", code, err)
	}
	// 换取码本身不能当会话用
	if _, err := store.Validate(ctx, code, ""); err == nil {
		t.Fatal("exchange code must not be usable as a session token")
	}

	sess, err := svc.ExchangeOAuthCode(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if sess.AccessToken == "" {
		t.Fatal("no access token")
	}
	if _, err := store.Validate(ctx, sess.AccessToken, ""); err != nil {
		t.Fatalf("exchanged session should validate: %v", err)
	}
	if sess.PlayerName == "" || sess.PlayerID == "" {
		t.Fatalf("session should carry the active player: %+v", sess)
	}

	// 单次消费：重放必须失败
	if _, err := svc.ExchangeOAuthCode(ctx, code); !errors.Is(err, yggdrasil.ErrInvalidToken) {
		t.Fatalf("replayed code: %v", err)
	}
	// 伪造码
	if _, err := svc.ExchangeOAuthCode(ctx, "not-a-code"); !errors.Is(err, yggdrasil.ErrInvalidToken) {
		t.Fatalf("forged code: %v", err)
	}
}

func TestOAuthLoginCodeErrors(t *testing.T) {
	ctx := context.Background()
	svc, _ := newOAuthTestService(t)

	if _, err := svc.OAuthLoginCode(ctx, "nope", "good"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := svc.OAuthLoginCode(ctx, "github", "bad"); err == nil {
		t.Fatal("bad provider code should fail")
	}
}

// 回跳 URL 拼接：目标已带查询串时不能出现两个 "?"。
func TestJoinQuery(t *testing.T) {
	cases := map[string]string{
		"https://web.test/profile":      "https://web.test/profile?oauth_code=x",
		"https://web.test/profile?a=1":  "https://web.test/profile?a=1&oauth_code=x",
		"https://web.test/login?next=/": "https://web.test/login?next=/&oauth_code=x",
	}
	for target, want := range cases {
		if got := joinQuery(target, "oauth_code=x"); got != want {
			t.Errorf("joinQuery(%q) = %q, want %q", target, got, want)
		}
	}
}
