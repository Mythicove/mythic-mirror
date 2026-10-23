package account

import (
	"context"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/mc"
)

func TestSkinLibFlow(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})

	item1, err := store.PublishSkin(ctx, "u1", "Alice", "tex1.png", "cool skin", "default")
	if err != nil {
		t.Fatal(err)
	}
	item2, err := store.PublishSkin(ctx, "u2", "Bob", "tex2.png", "another", "slim")
	if err != nil {
		t.Fatal(err)
	}
	if item1.CreatedAt == "" {
		t.Fatal("created_at missing")
	}
	_ = store.LikeSkin(ctx, item2.ID)
	_ = store.LikeSkin(ctx, item2.ID)

	// 最新在前
	items, total, err := svc.ListSkinLib(ctx, "", "newest", 0, 20)
	if err != nil || total != 2 || len(items) != 2 || items[0].ID != item2.ID {
		t.Fatalf("newest: items=%+v total=%d err=%v", items, total, err)
	}
	// 按点赞排序
	items, total, err = svc.ListSkinLib(ctx, "", "likes", 0, 20)
	if err != nil || total != 2 || items[0].ID != item2.ID || items[0].Likes != 2 {
		t.Fatalf("likes: items=%+v total=%d err=%v", items, total, err)
	}
	// 关键词过滤（描述命中）
	items, total, err = svc.ListSkinLib(ctx, "cool", "newest", 0, 20)
	if err != nil || total != 1 || items[0].ID != item1.ID {
		t.Fatalf("query: items=%+v total=%d err=%v", items, total, err)
	}
	// 分页
	items, total, err = svc.ListSkinLib(ctx, "", "newest", 1, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != item1.ID {
		t.Fatalf("page: items=%+v total=%d err=%v", items, total, err)
	}
	// limit 钳制
	_, _, err = svc.ListSkinLib(ctx, "", "bogus", -5, 9999)
	if err != nil {
		t.Fatal(err)
	}

	// 详情
	got, err := svc.GetSkinLibItem(ctx, item1.ID)
	if err != nil || got.PlayerName != "Alice" {
		t.Fatalf("get: %+v err=%v", got, err)
	}
	if _, err := svc.GetSkinLibItem(ctx, "nope"); err != yggdrasil.ErrNotFound {
		t.Fatalf("get missing: %v", err)
	}

	// 评论：空内容拒绝，正常写入可读，管理删除
	sctx := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: "u1"})
	if err := svc.CommentSkin(sctx, item1.ID, "   "); err != ErrInvalidComment {
		t.Fatalf("empty comment: %v", err)
	}
	if err := svc.CommentSkin(sctx, item1.ID, " nice! "); err != nil {
		t.Fatal(err)
	}
	comments, err := svc.ListComments(ctx, item1.ID)
	if err != nil || len(comments) != 1 || comments[0].Content != "nice!" {
		t.Fatalf("comments: %+v err=%v", comments, err)
	}
	if err := store.DeleteSkinComment(ctx, comments[0].ID); err != nil {
		t.Fatal(err)
	}
	comments, _ = svc.ListComments(ctx, item1.ID)
	if len(comments) != 0 {
		t.Fatalf("comment not deleted: %+v", comments)
	}
	if err := store.DeleteSkinComment(ctx, "nope"); err != yggdrasil.ErrNotFound {
		t.Fatalf("delete missing: %v", err)
	}
	// 评论不存在的条目 → 404 语义
	if err := svc.CommentSkin(sctx, "nope", "x"); err != yggdrasil.ErrNotFound {
		t.Fatalf("comment missing item: %v", err)
	}
}

func TestPublishQuota(t *testing.T) {
	ctx := context.Background()
	store := yggdrasil.NewMemoryStore()
	svc := newTestService(store, &captureMailer{})
	svc.SetQuota(yggdrasil.Quota{Published: 1})

	// 给种子玩家 Alice 设一张皮肤
	if err := store.SetSkin(ctx, mc.UUIDFromName("Alice"), "tex.png", "default"); err != nil {
		t.Fatal(err)
	}
	sctx := yggdrasil.WithSession(ctx, &yggdrasil.Session{UserID: "seed-admin"})
	if _, err := svc.PublishSkin(sctx, "Alice", "first"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if _, err := svc.PublishSkin(sctx, "Alice", "second"); err != yggdrasil.ErrQuotaExceeded {
		t.Fatalf("second publish err=%v, want ErrQuotaExceeded", err)
	}
}
