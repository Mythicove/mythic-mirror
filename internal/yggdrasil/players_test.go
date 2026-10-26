package yggdrasil

import (
	"context"
	"testing"

	"github.com/Mythicove/shared/mc"
)

// 玩家改名/删除/当前玩家选择的语义（内存实现；PG 侧见 pgstore 集成测试）。
func TestPlayerRenameDeleteActivate(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore() // 种子：seed-admin + 玩家 Alice
	const uid = "seed-admin"
	aliceID := mc.UUIDFromName("Alice")
	bobID := mc.UUIDFromName("Bob")

	if _, err := m.AddPlayer(ctx, uid, "Bob"); err != nil {
		t.Fatal(err)
	}

	// 未选当前玩家 → 会话用第一个
	tok, err := m.CreateSession(ctx, uid, "ct")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := m.Validate(ctx, tok, "")
	if err != nil || sess.PlayerID != aliceID {
		t.Fatalf("default session player: %+v err=%v", sess, err)
	}

	// 选 Bob 为当前玩家 → 新会话用 Bob；非本人玩家拒绝
	if err := m.SetActivePlayer(ctx, uid, "someone-elses-uuid"); err != ErrNotFound {
		t.Fatalf("foreign player activate: %v", err)
	}
	if err := m.SetActivePlayer(ctx, uid, bobID); err != nil {
		t.Fatal(err)
	}
	tokBob, err := m.CreateSession(ctx, uid, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := m.Validate(ctx, tokBob, ""); err != nil || sess.PlayerID != bobID {
		t.Fatalf("active session player: %+v err=%v", sess, err)
	}
	players, err := m.PlayersByUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	actives := 0
	for _, p := range players {
		if p.Active {
			actives++
			if p.Name != "Bob" {
				t.Fatalf("wrong player marked active: %+v", p)
			}
		}
	}
	if actives != 1 {
		t.Fatalf("expected exactly one active player, got %d (%+v)", actives, players)
	}

	// 改名：uuid 随名字重算，旧会话失效，active 跟随迁移
	renamed, err := m.RenamePlayer(ctx, uid, "Bob", "Bobby")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != mc.UUIDFromName("Bobby") {
		t.Fatalf("renamed uuid: %+v", renamed)
	}
	if _, err := m.Validate(ctx, tokBob, ""); err != ErrInvalidToken {
		t.Fatalf("stale session should be invalid after rename: %v", err)
	}
	tokAfter, err := m.CreateSession(ctx, uid, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := m.Validate(ctx, tokAfter, ""); err != nil || sess.PlayerID != renamed.ID {
		t.Fatalf("active should follow rename: %+v err=%v", sess, err)
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

	// 删除：非最后一个可删；最后一个被拒；active 清除后回到第一个
	if err := m.DeletePlayer(ctx, uid, "Bobby"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeletePlayer(ctx, uid, "Alice"); err != ErrLastPlayer {
		t.Fatalf("delete last player: %v", err)
	}
	if err := m.DeletePlayer(ctx, uid, "Bobby"); err != ErrNotFound {
		t.Fatalf("delete missing player: %v", err)
	}
	players, err = m.PlayersByUser(ctx, uid)
	if err != nil || len(players) != 1 || players[0].Name != "Alice" || players[0].Active {
		t.Fatalf("players after delete: %+v err=%v", players, err)
	}
	tokFinal, err := m.CreateSession(ctx, uid, "ct")
	if err != nil {
		t.Fatal(err)
	}
	if sess, err := m.Validate(ctx, tokFinal, ""); err != nil || sess.PlayerID != aliceID {
		t.Fatalf("session after deleting active: %+v err=%v", sess, err)
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
