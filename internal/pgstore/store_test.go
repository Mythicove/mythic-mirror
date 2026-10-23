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
