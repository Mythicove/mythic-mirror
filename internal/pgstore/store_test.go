package pgstore

import (
	"context"
	"os"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/passwd"
	"github.com/Mythicove/shared/mc"
)

// 需要 MIRROR_TEST_DATABASE_URL（先 goose 迁移到空库）才会运行。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIRROR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MIRROR_TEST_DATABASE_URL not set")
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

	// 清理残留
	_, _ = st.pool.Exec(ctx, `DELETE FROM players WHERE name = 'Alice'`)
	_, _ = st.pool.Exec(ctx, `DELETE FROM users WHERE email = 'admin@example.com'`)

	// 种子用户 + 玩家
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

	// Authenticate
	gotUID, err := st.Authenticate(ctx, "admin@example.com", "password")
	if err != nil || gotUID != uid {
		t.Fatalf("authenticate: %v %q", err, gotUID)
	}
	if _, err := st.Authenticate(ctx, "admin@example.com", "wrong"); err == nil {
		t.Fatal("expected invalid credentials")
	}

	// Player + session
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

	// SetSkin + ProfileByUUID
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

	// BindServer + HasJoined
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
