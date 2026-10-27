package yggdrasil

import (
	"context"
	"testing"

	"github.com/Mythicove/shared/mc"
)

// 玩家改名/删除的语义（内存实现；PG 侧见 pgstore 集成测试）。
// 「当前玩家」指针已随 ADR-0012 删除：会话由调用方指定玩家（启动器登录时就是认证到的那个）。
func TestPlayerRenameDelete(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore() // 种子：seed-admin + 玩家 Alice（含一条独立密码）
	const uid = "seed-admin"
	bobID := mc.UUIDFromName("Bob")

	if _, err := m.AddPlayer(ctx, uid, "Bob", 0); err != nil {
		t.Fatal(err)
	}
	// 玩家数上限由策略给出；limit>0 时超限拒绝
	if _, err := m.AddPlayer(ctx, uid, "Carol", 2); err != ErrPlayerLimit {
		t.Fatalf("player limit: %v", err)
	}

	tokBob, err := m.CreateSession(ctx, uid, bobID, "", "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := m.Validate(ctx, tokBob, ""); err != nil || sess.PlayerID != bobID {
		t.Fatalf("session player: %+v err=%v", sess, err)
	}

	// 独立密码属于玩家本身：改名换 uuid 后仍然跟着走（对齐 PG 的 ON UPDATE CASCADE）
	if _, err := m.CreatePlayerCredential(ctx, bobID, "laptop", "hash-1"); err != nil {
		t.Fatal(err)
	}
	renamed, err := m.RenamePlayer(ctx, uid, "Bob", "Bobby")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != mc.UUIDFromName("Bobby") {
		t.Fatalf("renamed uuid: %+v", renamed)
	}
	if creds, err := m.ListPlayerCredentials(ctx, renamed.ID); err != nil || len(creds) != 1 {
		t.Fatalf("credential should follow rename: %+v err=%v", creds, err)
	}
	// 旧 uuid 的会话已无对应档案，一律失效（启动器需重新登录）
	if _, err := m.Validate(ctx, tokBob, ""); err != ErrInvalidToken {
		t.Fatalf("stale session should be invalid after rename: %v", err)
	}
	// 名字冲突 / 别人的玩家
	if _, err := m.RenamePlayer(ctx, uid, "Bobby", "Alice"); err != ErrAlreadyExists {
		t.Fatalf("rename to taken name: %v", err)
	}
	if _, err := m.RenamePlayer(ctx, "nobody", "Alice", "Alicia"); err != ErrNotFound {
		t.Fatalf("rename foreign player: %v", err)
	}

	// 已绑定正版 → 拒绝改名（名字来自 Mojang）
	if err := m.SetPremium(ctx, renamed.ID, "premium-uuid"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RenamePlayer(ctx, uid, "Bobby", "Bobbie"); err != ErrPremiumBound {
		t.Fatalf("premium rename: %v", err)
	}

	// 删除：非最后一个可删（连同其独立密码）；最后一个被拒
	if err := m.DeletePlayer(ctx, uid, "Bobby"); err != nil {
		t.Fatal(err)
	}
	if creds, err := m.ListPlayerCredentials(ctx, renamed.ID); err != nil || len(creds) != 0 {
		t.Fatalf("credentials should be dropped with the player: %+v err=%v", creds, err)
	}
	if err := m.DeletePlayer(ctx, uid, "Alice"); err != ErrLastPlayer {
		t.Fatalf("delete last player: %v", err)
	}
	if err := m.DeletePlayer(ctx, uid, "Bobby"); err != ErrNotFound {
		t.Fatalf("delete missing player: %v", err)
	}
	players, err := m.PlayersByUser(ctx, uid)
	if err != nil || len(players) != 1 || players[0].Name != "Alice" {
		t.Fatalf("players after delete: %+v err=%v", players, err)
	}
}

// 正版绑定标记的暴露与解绑语义：绑定后禁止本地改名，解绑（清空 premium_uuid）后恢复可改。
func TestPremiumFlagAndUnbind(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore() // 种子：seed-admin + Alice
	const uid = "seed-admin"
	aliceID := mc.UUIDFromName("Alice")

	if err := m.SetPremium(ctx, aliceID, "premium-1"); err != nil {
		t.Fatal(err)
	}
	ps, err := m.PlayersByUser(ctx, uid)
	if err != nil || len(ps) != 1 || !ps[0].Premium {
		t.Fatalf("premium flag should be exposed: %+v err=%v", ps, err)
	}
	if _, err := m.RenamePlayer(ctx, uid, "Alice", "Alicia"); err != ErrPremiumBound {
		t.Fatalf("bound player must not be renamable: %v", err)
	}

	if err := m.SetPremium(ctx, aliceID, ""); err != nil {
		t.Fatal(err)
	}
	ps, err = m.PlayersByUser(ctx, uid)
	if err != nil || ps[0].Premium {
		t.Fatalf("premium flag should clear after unbind: %+v err=%v", ps, err)
	}
	if _, err := m.RenamePlayer(ctx, uid, "Alice", "Alicia"); err != nil {
		t.Fatalf("rename after unbind: %v", err)
	}
}
