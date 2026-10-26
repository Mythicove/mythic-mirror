package yggdrasil

import (
	"context"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/shared/mc"
)

// Authenticate 返回的 selectedProfile 必须与会话固定的玩家一致（都遵循"当前玩家"）。
// 回归：此前 selectedProfile 硬取 players[0]，而 CreateSession 用当前玩家，
// 导致启动器显示的身份与实际会话不符，删掉当前玩家会让令牌立刻失效。
func TestAuthenticateSelectsActivePlayer(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, kp, fs, ServiceOptions{ServerName: "test", ImplementationName: "test"})

	const uid = "seed-admin"
	if _, err := store.AddPlayer(ctx, uid, "Bob"); err != nil {
		t.Fatal(err)
	}
	bobID := mc.UUIDFromName("Bob")
	if err := store.SetActivePlayer(ctx, uid, bobID); err != nil {
		t.Fatal(err)
	}

	resp, err := svc.Authenticate(ctx, AuthenticateRequest{
		Username: "admin", Password: "password", ClientToken: "ct",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SelectedProfile == nil || resp.SelectedProfile.Name != "Bob" {
		t.Fatalf("selected profile should be the active player: %+v", resp.SelectedProfile)
	}
	sess, err := store.Validate(ctx, resp.AccessToken, "ct")
	if err != nil || sess.PlayerID != bobID {
		t.Fatalf("session should point at the same player: %+v err=%v", sess, err)
	}
	if len(resp.AvailableProfiles) != 2 {
		t.Fatalf("available profiles should still list all players: %+v", resp.AvailableProfiles)
	}
}
