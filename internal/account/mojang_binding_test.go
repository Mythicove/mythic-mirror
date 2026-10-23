package account

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/memcache"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/oauth"
)

type fakeMSOAuth struct{}

func (fakeMSOAuth) AuthURL(state string) string {
	return "https://login.microsoftonline.com/auth?state=" + state
}

func (fakeMSOAuth) Exchange(context.Context, string) (*oauth.Profile, error) {
	return &oauth.Profile{Provider: "microsoft", UID: "ms-uuid-1", Nickname: "Alice"}, nil
}

// 副本 A 发起绑定、副本 B 完成回调：state 经共享 cache 传递（多副本正确性）。
func TestMojangBindingAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	shared := memcache.New()

	mk := func() *Service {
		s := NewService(store, &captureMailer{}, "http://localhost", shared)
		s.Configure(Options{MojangOAuth: fakeMSOAuth{}})
		return s
	}
	replicaA, replicaB := mk(), mk()

	sess := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: "seed-admin"})
	authURL, err := replicaA.MojangOAuthAuthURL(sess)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	state := u.Query().Get("state")
	if state == "" || !strings.HasPrefix(authURL, "https://login.microsoftonline.com/") {
		t.Fatalf("bad auth url %q", authURL)
	}

	// 另一个副本消费 state 完成绑定
	if err := replicaB.MojangOAuthCallback(ctx, state, "code"); err != nil {
		t.Fatal(err)
	}
	pd, err := store.ProfileByName(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if pd.PremiumUUID != "ms-uuid-1" {
		t.Fatalf("premium not bound: %+v", pd)
	}

	// state 已消费，重放拒绝
	if err := replicaB.MojangOAuthCallback(ctx, state, "code"); err != ErrInvalidState {
		t.Fatalf("replay: %v", err)
	}
	// 伪造 state 拒绝
	if err := replicaB.MojangOAuthCallback(ctx, "bogus", "code"); err != ErrInvalidState {
		t.Fatalf("bogus state: %v", err)
	}
}
