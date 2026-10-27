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

	token, err := st.CreateSession(ctx, uid, aliceUUID, "", "client-1")
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
	item1, err := st.PublishSkin(ctx, uid, "LibA", "texA.png", token+" red dragon", "default", "")
	if err != nil {
		t.Fatal(err)
	}
	item2, err := st.PublishSkin(ctx, uid, "LibB", "texB.png", token+" blue", "slim", "")
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
	before, total, err := st.ListAudit(ctx, "", 10, 0)
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
	if _, err := st.PublishSkin(ctx, uid, "QuotaPlayer", "tex-a.png", "d", "default", ""); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountPublished(ctx, uid)
	if err != nil || n != 1 {
		t.Fatalf("published=%d err=%v", n, err)
	}
}

func TestReportsAndReviewPG(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	_, _ = st.pool.Exec(ctx, `DELETE FROM reports WHERE target_id LIKE 'rep-test%'`)
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = 'reporter@example.com'`)
	hash, _ := passwd.Hash("password")
	var uid string
	if err := st.pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		"reporter@example.com", hash, "Reporter").Scan(&uid); err != nil {
		t.Fatal(err)
	}

	// 举报 + 重复拒绝
	if err := st.ReportTarget(ctx, uid, "skin", "rep-test-1", "spam"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReportTarget(ctx, uid, "skin", "rep-test-1", "again"); err != yggdrasil.ErrAlreadyExists {
		t.Fatalf("duplicate: %v", err)
	}
	if err := st.ReportTarget(ctx, uid, "comment", "rep-test-2", ""); err != nil {
		t.Fatal(err)
	}
	reports, total, err := st.ListReports(ctx, "pending", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total < 2 {
		t.Fatalf("pending total=%d", total)
	}
	if reports[0].Status != "pending" {
		t.Fatalf("first report status=%q", reports[0].Status)
	}
	if reports[0].ReporterName != "Reporter" {
		t.Fatalf("reporter name=%q", reports[0].ReporterName)
	}
	var rid string
	for _, r := range reports {
		if r.TargetID == "rep-test-1" {
			rid = r.ID
		}
	}
	if err := st.ResolveReport(ctx, rid, "admin-x", "acted"); err != nil {
		t.Fatal(err)
	}
	all, _, _ := st.ListReports(ctx, "", 50, 0)
	found := false
	for _, r := range all {
		if r.ID == rid {
			found = true
			if r.Status != "acted" || r.ReviewedBy != "admin-x" || r.ReviewedAt == "" {
				t.Fatalf("resolved: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("resolved report missing")
	}
	if err := st.ResolveReport(ctx, "nope", "a", "acted"); err != yggdrasil.ErrNotFound {
		t.Fatalf("resolve missing: %v", err)
	}

	// 审核队列：under_review 不入公开列表，管理端可见并可批准
	item, err := st.PublishSkin(ctx, uid, "ReviewPlayer", "tex-r.png", "d", "default", "under_review")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = st.pool.Exec(ctx, `DELETE FROM skinlib_items WHERE id = $1`, item.ID) }()
	pub, _, err := st.ListSkinLib(ctx, "", "newest", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range pub {
		if it.ID == item.ID {
			t.Fatal("under_review leaked to public list")
		}
	}
	_, adminTotal, err := st.AdminListSkins(ctx, "under_review", 10, 0)
	if err != nil || adminTotal < 1 {
		t.Fatalf("admin list total=%d err=%v", adminTotal, err)
	}
	if err := st.SetSkinStatus(ctx, item.ID, "listed"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSkinLibItem(ctx, item.ID)
	if err != nil || got.Status != "listed" {
		t.Fatalf("after approve: %+v err=%v", got, err)
	}
	if err := st.SetSkinStatus(ctx, "nope", "listed"); err != yggdrasil.ErrNotFound {
		t.Fatalf("status missing: %v", err)
	}
}

// 玩家改名/删除（PG 侧）：uuid 由名字派生，故改名会换主键并让旧会话失效；
// 独立密码属于玩家本身，跟随新 uuid（外键 ON UPDATE CASCADE）。
func TestPlayersRenameDeletePG(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const email = "players-lifecycle@example.com"
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	hash, _ := passwd.Hash("password")
	var uid string
	if err := st.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, nickname) VALUES ($1, $2, 'P') RETURNING id`,
		email, hash).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid) }()

	// 名字全局唯一：清掉历史残留，保证可重复运行
	_, _ = st.pool.Exec(ctx, `DELETE FROM players WHERE name IN ('PaLifecycle','PbLifecycle','PbRenamed','PbRenamed2','PaRenamed')`)

	alice, err := st.AddPlayer(ctx, uid, "PaLifecycle", 0)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.AddPlayer(ctx, uid, "PbLifecycle", 0)
	if err != nil {
		t.Fatal(err)
	}
	// 玩家数上限由策略给出（limit<=0 表示不限）
	if _, err := st.AddPlayer(ctx, uid, "PcLifecycle", 2); err != yggdrasil.ErrPlayerLimit {
		t.Fatalf("player limit: %v", err)
	}

	// 会话由调用方指定玩家（启动器登录时就是认证到的那个）；网站会话 player_id 为空
	tokBob, err := st.CreateSession(ctx, uid, bob.ID, "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := st.Validate(ctx, tokBob, ""); err != nil || sess.PlayerID != bob.ID {
		t.Fatalf("session player: %+v err=%v", sess, err)
	}
	tokWeb, err := st.CreateSession(ctx, uid, "", "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := st.Validate(ctx, tokWeb, ""); err != nil || sess.PlayerID != "" {
		t.Fatalf("website session must not pin a player: %+v err=%v", sess, err)
	}

	// 独立密码属于玩家本身：改名换 uuid 后仍跟着走（外键 ON UPDATE CASCADE）
	credHash, _ := passwd.Hash("launcher-pw")
	cred, err := st.CreatePlayerCredential(ctx, bob.ID, "laptop", credHash)
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := st.RenamePlayer(ctx, uid, "PbLifecycle", "PbRenamed")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != mc.UUIDFromName("PbRenamed") {
		t.Fatalf("renamed uuid: %+v", renamed)
	}
	if creds, err := st.ListPlayerCredentials(ctx, renamed.ID); err != nil || len(creds) != 1 || creds[0].ID != cred.ID {
		t.Fatalf("credential should follow rename: %+v err=%v", creds, err)
	}
	if _, err := st.Validate(ctx, tokBob, ""); err != yggdrasil.ErrInvalidToken {
		t.Fatalf("stale session after rename: %v", err)
	}
	if _, err := st.RenamePlayer(ctx, uid, "PbRenamed", "PaLifecycle"); err != yggdrasil.ErrAlreadyExists {
		t.Fatalf("rename to taken name: %v", err)
	}
	if _, err := st.RenamePlayer(ctx, "nobody", "PaLifecycle", "PaRenamed"); err != yggdrasil.ErrNotFound {
		t.Fatalf("rename foreign player: %v", err)
	}

	// 已绑定正版 → 拒绝改名
	if err := st.SetPremium(ctx, renamed.ID, "premium-uuid"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RenamePlayer(ctx, uid, "PbRenamed", "PbRenamed2"); err != yggdrasil.ErrPremiumBound {
		t.Fatalf("premium rename: %v", err)
	}
	_ = st.SetPremium(ctx, renamed.ID, "")

	// 撤销独立密码 → 由它建立的会话经外键级联一并销毁
	tokCred, err := st.CreateSession(ctx, uid, renamed.ID, cred.ID, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Validate(ctx, tokCred, ""); err != nil {
		t.Fatalf("credential session should validate: %v", err)
	}
	if err := st.DeletePlayerCredential(ctx, renamed.ID, cred.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Validate(ctx, tokCred, ""); err != yggdrasil.ErrInvalidToken {
		t.Fatalf("revoking a credential must kill its sessions: %v", err)
	}
	if err := st.DeletePlayerCredential(ctx, renamed.ID, cred.ID); err != yggdrasil.ErrNotFound {
		t.Fatalf("delete missing credential: %v", err)
	}

	// 删除：非最后一个可删（连同其独立密码），最后一个被拒
	if _, err := st.CreatePlayerCredential(ctx, renamed.ID, "desktop", credHash); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePlayer(ctx, uid, "PbRenamed"); err != nil {
		t.Fatal(err)
	}
	if creds, err := st.ListPlayerCredentials(ctx, renamed.ID); err != nil || len(creds) != 0 {
		t.Fatalf("credentials should be dropped with the player: %+v err=%v", creds, err)
	}
	if err := st.DeletePlayer(ctx, uid, "PaLifecycle"); err != yggdrasil.ErrLastPlayer {
		t.Fatalf("delete last player: %v", err)
	}
	if err := st.DeletePlayer(ctx, uid, "PbRenamed"); err != yggdrasil.ErrNotFound {
		t.Fatalf("delete missing: %v", err)
	}
	if players, err := st.PlayersByUser(ctx, uid); err != nil || len(players) != 1 || players[0].ID != alice.ID {
		t.Fatalf("players after delete: %+v err=%v", players, err)
	}
}

// 启动器独立密码与第三方绑定（PG 侧，ADR-0012）：
// 启动器登录只认「玩家名 + 该玩家的独立密码」，主密码不参与；
// 撤销凭证经外键级联销毁会话；封禁账号既不能用主密码登录也不能进服；
// CountPasswordOnlyUsers 只数「有主密码、无绑定、已验证且状态正常」的账号。
func TestLauncherCredentialsPG(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const email = "launcher-creds@example.com"
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	_, _ = st.pool.Exec(ctx, `DELETE FROM players WHERE name IN ('LcAlice','LcBob')`)
	masterHash, _ := passwd.Hash("master-pw")
	var uid string
	if err := st.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, nickname, email_verified) VALUES ($1, $2, 'L', true) RETURNING id`,
		email, masterHash).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid) }()

	alice, err := st.AddPlayer(ctx, uid, "LcAlice", 0)
	if err != nil {
		t.Fatal(err)
	}

	// 没有独立密码时，主密码也不能从启动器登录（这是本次拆分的核心）
	if _, err := st.AuthenticatePlayer(ctx, "LcAlice", "master-pw"); !errors.Is(err, yggdrasil.ErrInvalidCredentials) {
		t.Fatalf("master password must not work at the launcher: %v", err)
	}
	// 邮箱也不能当启动器用户名
	if _, err := st.AuthenticatePlayer(ctx, email, "master-pw"); !errors.Is(err, yggdrasil.ErrInvalidCredentials) {
		t.Fatalf("email must not work as a launcher username: %v", err)
	}

	credHash, _ := passwd.Hash("launcher-pw")
	cred, err := st.CreatePlayerCredential(ctx, alice.ID, "desktop", credHash)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Name != "desktop" || cred.CreatedAt == "" || cred.LastUsedAt != "" {
		t.Fatalf("unexpected credential view: %+v", cred)
	}
	auth, err := st.AuthenticatePlayer(ctx, "LcAlice", "launcher-pw")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Player.ID != alice.ID || auth.Player.Name != "LcAlice" || auth.UserID != uid || auth.CredentialID != cred.ID {
		t.Fatalf("launcher auth result: %+v", auth)
	}
	if _, err := st.AuthenticatePlayer(ctx, "LcAlice", "wrong"); !errors.Is(err, yggdrasil.ErrInvalidCredentials) {
		t.Fatalf("wrong launcher password: %v", err)
	}
	creds, err := st.ListPlayerCredentials(ctx, alice.ID)
	if err != nil || len(creds) != 1 || creds[0].LastUsedAt == "" {
		t.Fatalf("last_used_at should be stamped: %+v err=%v", creds, err)
	}

	// 撤销凭证 → 由它建立的会话立即失效（外键 ON DELETE CASCADE）
	tok, err := st.CreateSession(ctx, uid, alice.ID, cred.ID, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := st.Validate(ctx, tok, ""); err != nil || sess.CredentialID != cred.ID {
		t.Fatalf("credential session: %+v err=%v", sess, err)
	}
	if err := st.DeletePlayerCredential(ctx, alice.ID, cred.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Validate(ctx, tok, ""); !errors.Is(err, yggdrasil.ErrInvalidToken) {
		t.Fatalf("revoking a credential must kill its sessions: %v", err)
	}
	// 网站会话（无玩家、无凭证）不受影响
	tokWeb, err := st.CreateSession(ctx, uid, "", "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := st.Validate(ctx, tokWeb, ""); err != nil || sess.PlayerID != "" || sess.CredentialID != "" {
		t.Fatalf("website session: %+v err=%v", sess, err)
	}

	// 封禁账号：主密码登录与启动器登录都必须拒
	if _, err := st.CreatePlayerCredential(ctx, alice.ID, "desktop", credHash); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE users SET status = 'banned' WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(ctx, email, "master-pw"); !errors.Is(err, yggdrasil.ErrInvalidCredentials) {
		t.Fatalf("banned user must not log in with the master password: %v", err)
	}
	if _, err := st.AuthenticatePlayer(ctx, "LcAlice", "launcher-pw"); !errors.Is(err, yggdrasil.ErrAccountDisabled) {
		t.Fatalf("banned user must not enter a server: %v", err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE users SET status = 'active' WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}

	// 绑定第三方后不再算"纯密码账号"；upsert 幂等（同 provider+subject 不产生第二行）
	before, err := st.CountPasswordOnlyUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertIdentity(ctx, "github", "gh-1", uid, email); err != nil {
		t.Fatal(err)
	}
	if after, err := st.CountPasswordOnlyUsers(ctx); err != nil || after != before-1 {
		t.Fatalf("binding must remove the account from the password-only count: %d -> %d err=%v", before, after, err)
	}
	if err := st.UpsertIdentity(ctx, "github", "gh-1", uid, "changed@example.com"); err != nil {
		t.Fatal(err)
	}
	ids, err := st.Identities(ctx, uid)
	if err != nil || len(ids) != 1 || ids[0].Provider != "github" || ids[0].Email != "changed@example.com" {
		t.Fatalf("identities: %+v err=%v", ids, err)
	}
	if got, err := st.IdentityUser(ctx, "github", "gh-1"); err != nil || got != uid {
		t.Fatalf("identity lookup: %q err=%v", got, err)
	}
	if _, err := st.IdentityUser(ctx, "github", "nobody"); !errors.Is(err, yggdrasil.ErrNotFound) {
		t.Fatalf("unknown identity: %v", err)
	}
	if _, err := st.Authenticate(ctx, email, "master-pw"); err != nil {
		t.Fatalf("binding must not break master-password verification itself: %v", err)
	}

	// Signout 用玩家名 + 独立密码，销毁该玩家的游戏会话（网站会话不动）
	tokGame, err := st.CreateSession(ctx, uid, alice.ID, "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Signout(ctx, "LcAlice", "launcher-pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Validate(ctx, tokGame, ""); !errors.Is(err, yggdrasil.ErrInvalidToken) {
		t.Fatalf("signout must drop the player's game sessions: %v", err)
	}
	if _, err := st.Validate(ctx, tokWeb, ""); err != nil {
		t.Fatalf("signout must not touch website sessions: %v", err)
	}
	if err := st.Signout(ctx, "LcAlice", "master-pw"); !errors.Is(err, yggdrasil.ErrInvalidCredentials) {
		t.Fatalf("signout must not accept the master password: %v", err)
	}
}
