package pgstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/migrate"
	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mc"
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
