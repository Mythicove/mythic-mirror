package pgstore

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/migrate"
	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mc"
	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/openid"
	"github.com/ory/fosite/token/jwt"
)

// 需要 MIRROR_TEST_DATABASE_URL（指向空库/测试库）才会运行；迁移由测试自动执行。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIRROR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MIRROR_TEST_DATABASE_URL not set")
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return dsn
}

func TestStoreIntegration(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, _ = st.pool.Exec(ctx, `DELETE FROM players WHERE name = 'Alice'`)
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = 'admin@example.com'`)

	hash, _ := passwd.Hash("password")
	var uid string
	if err := st.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		"admin@example.com", hash, "Admin").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	aliceUUID := mc.UUIDFromName("Alice")
	if _, err := st.pool.Exec(ctx, `INSERT INTO players (uuid, name, owner_user_id) VALUES ($1,$2,$3)`,
		aliceUUID, "Alice", uid); err != nil {
		t.Fatal(err)
	}

	gotUID, err := st.Authenticate(ctx, "admin@example.com", "password")
	if err != nil || gotUID != uid {
		t.Fatalf("authenticate: %v %q", err, gotUID)
	}
	if _, err := st.Authenticate(ctx, "admin@example.com", "wrong"); err == nil {
		t.Fatal("expected invalid credentials")
	}

	if _, err := st.PlayerByUser(ctx, uid); err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(ctx, uid, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := st.Validate(ctx, token, "client-1")
	if err != nil || sess.PlayerID != aliceUUID {
		t.Fatalf("validate: %v %+v", err, sess)
	}

	if err := st.SetSkin(ctx, aliceUUID, "abc.png", "slim"); err != nil {
		t.Fatal(err)
	}
	pd, err := st.ProfileByUUID(ctx, aliceUUID)
	if err != nil {
		t.Fatal(err)
	}
	if pd.SkinKey != "abc.png" || pd.Model != "slim" {
		t.Fatalf("profile: %+v", pd)
	}

	if err := st.BindServer(ctx, token, aliceUUID, "srv1"); err != nil {
		t.Fatal(err)
	}
	pd2, err := st.HasJoined(ctx, "Alice", "srv1")
	if err != nil {
		t.Fatal(err)
	}
	if pd2.Player.Name != "Alice" {
		t.Fatalf("hasJoined: %+v", pd2)
	}
}

func TestAccountIntegration(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, _ = st.pool.Exec(ctx, `DELETE FROM players WHERE name = 'Carol'`)
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = 'carol@example.com'`)

	hash, _ := passwd.Hash("password123")
	acct, err := st.CreateAccount(ctx, "carol@example.com", hash, "Carol", "Carol")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(ctx, "carol@example.com", hash, "Carol2", "Carol2"); err != yggdrasil.ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	tok, err := st.CreateToken(ctx, acct.ID, yggdrasil.TokenVerify, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := st.ConsumeToken(ctx, yggdrasil.TokenVerify, tok)
	if err != nil || uid != acct.ID {
		t.Fatalf("consume: %v %q", err, uid)
	}
	if _, err := st.ConsumeToken(ctx, yggdrasil.TokenVerify, tok); err == nil {
		t.Fatal("expected token consumed")
	}

	if err := st.MarkVerified(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.AccountByEmail(ctx, "carol@example.com")
	if err != nil || !got.Verified {
		t.Fatalf("account=%+v err=%v", got, err)
	}

	if _, err := st.Authenticate(ctx, "carol@example.com", "password123"); err != nil {
		t.Fatal(err)
	}

	newHash, _ := passwd.Hash("newpassword1")
	if err := st.UpdatePassword(ctx, acct.ID, newHash); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(ctx, "carol@example.com", "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(ctx, "carol@example.com", "password123"); err == nil {
		t.Fatal("old password should fail")
	}
}

func TestSkinLibIntegration(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	hash, _ := passwd.Hash("password")
	var uid string
	email := "lib-" + mc.UUIDFromName("libuser")[:8] + "@example.com"
	if err := st.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		email, hash, "LibUser").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid) }()

	token := "libtest-" + mc.UUIDFromName(email)[:8]
	item1, err := st.PublishSkin(ctx, uid, "LibA", "texA.png", token+" red dragon", "default")
	if err != nil {
		t.Fatal(err)
	}
	item2, err := st.PublishSkin(ctx, uid, "LibB", "texB.png", token+" blue", "slim")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LikeSkin(ctx, item2.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.LikeSkin(ctx, "missing"); err != yggdrasil.ErrNotFound {
		t.Fatalf("like missing: %v", err)
	}

	// 按唯一 token 圈定本次数据：newest → item2 在前；likes → item2 在前
	items, total, err := st.ListSkinLib(ctx, token, "newest", 0, 10)
	if err != nil || total != 2 || items[0].ID != item2.ID {
		t.Fatalf("newest: total=%d items=%+v err=%v", total, items, err)
	}
	items, total, err = st.ListSkinLib(ctx, token, "likes", 0, 10)
	if err != nil || total != 2 || items[0].ID != item2.ID || items[0].Likes != 1 {
		t.Fatalf("likes: total=%d items=%+v err=%v", total, items, err)
	}
	items, total, err = st.ListSkinLib(ctx, token, "newest", 1, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != item1.ID {
		t.Fatalf("page: total=%d items=%+v err=%v", total, items, err)
	}
	if items[0].CreatedAt == "" {
		t.Fatal("created_at missing")
	}

	got, err := st.GetSkinLibItem(ctx, item1.ID)
	if err != nil || got.PlayerName != "LibA" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err := st.GetSkinLibItem(ctx, "missing"); err != yggdrasil.ErrNotFound {
		t.Fatalf("get missing: %v", err)
	}

	if err := st.CommentSkin(ctx, item1.ID, uid, "hello"); err != nil {
		t.Fatal(err)
	}
	comments, err := st.ListSkinComments(ctx, item1.ID)
	if err != nil || len(comments) != 1 || comments[0].Content != "hello" || comments[0].AuthorName != "LibUser" {
		t.Fatalf("comments: %+v err=%v", comments, err)
	}
	if err := st.DeleteSkinComment(ctx, comments[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSkinComment(ctx, comments[0].ID); err != yggdrasil.ErrNotFound {
		t.Fatalf("delete missing comment: %v", err)
	}
}

func TestAuditIntegration(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.WriteAudit(ctx, yggdrasil.AuditEntry{
		Actor: "user:u1", ActorIP: "203.0.113.9", Action: "admin.user.update", Target: "u2", Detail: "role=admin",
	}); err != nil {
		t.Fatal(err)
	}
	before, total, err := st.ListAudit(ctx, 10, 0)
	if err != nil || total < 1 || before[0].Action != "admin.user.update" || before[0].ActorIP != "203.0.113.9" {
		t.Fatalf("list: total=%d entries=%+v err=%v", total, before, err)
	}
	if before[0].CreatedAt == "" || before[0].ID == "" {
		t.Fatalf("missing id/created_at: %+v", before[0])
	}
}

func TestOIDCStoreIntegration(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	oc := st.OIDC()

	now := time.Now().UTC().Truncate(time.Second)
	req := &fosite.Request{
		ID:             "oidc-req-1",
		RequestedAt:    now,
		Client:         &fosite.DefaultClient{ID: "cli-1", RedirectURIs: []string{"https://app/cb"}, GrantTypes: fosite.Arguments{"authorization_code"}, ResponseTypes: fosite.Arguments{"code"}, Scopes: fosite.Arguments{"openid", "profile"}, Public: true},
		RequestedScope: fosite.Arguments{"openid", "profile"},
		GrantedScope:   fosite.Arguments{"openid"},
		Form:           url.Values{"redirect_uri": []string{"https://app/cb"}},
		Session: &openid.DefaultSession{
			Claims:    &jwt.IDTokenClaims{Subject: "user-1", Issuer: "http://issuer", Extra: map[string]any{"email": "a@b.c"}},
			Subject:   "user-1",
			ExpiresAt: map[fosite.TokenType]time.Time{fosite.AccessToken: now.Add(time.Hour)},
		},
	}

	// 授权码往返（含 session claims 保真）
	if err := oc.CreateAuthorizeCodeSession(ctx, "code-1", req); err != nil {
		t.Fatal(err)
	}
	got, err := oc.GetAuthorizeCodeSession(ctx, "code-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetID() != "oidc-req-1" || got.GetClient().GetID() != "cli-1" {
		t.Fatalf("roundtrip id/client: %s %s", got.GetID(), got.GetClient().GetID())
	}
	rs := got.GetRequestedScopes()
	if len(rs) != 2 || !rs.Has("openid", "profile") || !got.GetGrantedScopes().ExactOne("openid") {
		t.Fatalf("scopes: %v / %v", rs, got.GetGrantedScopes())
	}
	if got.GetRequestForm().Get("redirect_uri") != "https://app/cb" {
		t.Fatalf("form: %v", got.GetRequestForm())
	}
	sess := got.GetSession().(*openid.DefaultSession)
	if sess.Subject != "user-1" || sess.Claims.Extra["email"] != "a@b.c" {
		t.Fatalf("session: %+v", sess)
	}
	if !sess.GetExpiresAt(fosite.AccessToken).Equal(now.Add(time.Hour)) {
		t.Fatalf("session expiry: %v", sess.GetExpiresAt(fosite.AccessToken))
	}

	// 失活语义
	if err := oc.InvalidateAuthorizeCodeSession(ctx, "code-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetAuthorizeCodeSession(ctx, "code-1", nil); !errors.Is(err, fosite.ErrInvalidatedAuthorizeCode) {
		t.Fatalf("expected ErrInvalidatedAuthorizeCode, got %v", err)
	}
	if _, err := oc.GetAuthorizeCodeSession(ctx, "missing", nil); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := oc.InvalidateAuthorizeCodeSession(ctx, "missing"); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("invalidate missing: %v", err)
	}

	// access/refresh + 撤销/轮换
	if err := oc.CreateAccessTokenSession(ctx, "at-1", req); err != nil {
		t.Fatal(err)
	}
	if err := oc.CreateRefreshTokenSession(ctx, "rt-1", "at-1", req); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetRefreshTokenSession(ctx, "rt-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := oc.RevokeRefreshToken(ctx, "oidc-req-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetRefreshTokenSession(ctx, "rt-1", nil); !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("expected ErrInactiveToken, got %v", err)
	}
	if err := oc.RotateRefreshToken(ctx, "oidc-req-1", "rt-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetAccessTokenSession(ctx, "at-1", nil); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("expected access revoked, got %v", err)
	}

	// OIDC session 与 PKCE
	if err := oc.CreateOpenIDConnectSession(ctx, "code-9", req); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetOpenIDConnectSession(ctx, "code-9", nil); err != nil {
		t.Fatal(err)
	}
	if err := oc.DeleteOpenIDConnectSession(ctx, "code-9"); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetOpenIDConnectSession(ctx, "code-9", nil); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := oc.CreatePKCERequestSession(ctx, "code-8", req); err != nil {
		t.Fatal(err)
	}
	if _, err := oc.GetPKCERequestSession(ctx, "code-8", nil); err != nil {
		t.Fatal(err)
	}
	if err := oc.DeletePKCERequestSession(ctx, "code-8"); err != nil {
		t.Fatal(err)
	}
}

func TestUserStorageAndQuotaPG(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 独立测试用户（避免污染种子数据）；清理上次运行的纹理登记残留
	_, _ = st.pool.Exec(ctx, `DELETE FROM files WHERE key IN ('tex-a.png','tex-b.png')`)
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = 'quota@example.com'`)
	hash, _ := passwd.Hash("password")
	var uid string
	if err := st.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		"quota@example.com", hash, "Quota").Scan(&uid); err != nil {
		t.Fatal(err)
	}

	// 初始用量 0，tier 默认 free
	tier, used, err := st.UserStorage(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if tier != "free" || used != 0 {
		t.Fatalf("initial tier=%q used=%d", tier, used)
	}
	// 登记纹理 → 用量累计；同一 key 重复登记（sha256 去重）不重复计数
	if added, err := st.RecordTexture(ctx, "tex-a.png", uid, 1000); err != nil || !added {
		t.Fatalf("record tex-a: added=%v err=%v", added, err)
	}
	if added, err := st.RecordTexture(ctx, "tex-a.png", uid, 1000); err != nil || added {
		t.Fatalf("re-record tex-a: added=%v err=%v (want dedup)", added, err)
	}
	if _, err := st.RecordTexture(ctx, "tex-b.png", uid, 500); err != nil {
		t.Fatal(err)
	}
	// 撤销登记回滚用量
	if err := st.UnrecordTexture(ctx, "tex-b.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordTexture(ctx, "tex-b.png", uid, 500); err != nil {
		t.Fatal(err)
	}
	_, used, err = st.UserStorage(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if used != 1500 {
		t.Fatalf("used=%d, want 1500 (deduped)", used)
	}
	// 发布计数
	if _, err := st.PublishSkin(ctx, uid, "QuotaPlayer", "tex-a.png", "d", "default"); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountPublished(ctx, uid)
	if err != nil || n != 1 {
		t.Fatalf("published=%d err=%v", n, err)
	}
}
