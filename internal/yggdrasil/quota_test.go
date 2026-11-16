package yggdrasil

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/keys"
)

func makeSkinPNGColor(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 皮肤/披风写操作必须归属会话：非本人玩家与无会话一律拒绝（防"登录即改他人皮肤"）。
func TestUploadSkinRequiresOwnership(t *testing.T) {
	kp, _ := keys.Generate()
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewMemoryStore(), kp, fs, ServiceOptions{
		ServerName: "t", ImplementationName: "t", SkinDomains: []string{"localhost"},
	})
	skin := makeSkinPNGColor(t, 64, 64, color.RGBA{255, 0, 0, 255})

	// Alice 属种子账号 seed-admin；换个会话用户必须被拒
	other := WithSession(context.Background(), &Session{UserID: "eve"})
	if err := svc.UploadSkin(other, "Alice", "default", bytes.NewReader(skin)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner upload must be refused, got %v", err)
	}
	if err := svc.UploadCape(other, "Alice", bytes.NewReader(skin)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner cape upload must be refused, got %v", err)
	}
	if err := svc.ClearSkin(other, "Alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner clear must be refused, got %v", err)
	}
	// 无会话同样拒绝
	if err := svc.UploadSkin(context.Background(), "Alice", "default", bytes.NewReader(skin)); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("sessionless upload must be refused, got %v", err)
	}
	// 本人放行
	owner := WithSession(context.Background(), &Session{UserID: "seed-admin"})
	if err := svc.UploadSkin(owner, "Alice", "default", bytes.NewReader(skin)); err != nil {
		t.Fatalf("owner upload must pass: %v", err)
	}
	if err := svc.ClearSkin(owner, "Alice"); err != nil {
		t.Fatalf("owner clear must pass: %v", err)
	}
}

func TestUploadStorageQuota(t *testing.T) {
	// 上传须归属会话：种子账号 seed-admin 拥有玩家 Alice
	ctx := WithSession(context.Background(), &Session{UserID: "seed-admin"})
	kp, _ := keys.Generate()
	fs, err := filestorage.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	svc := NewService(store, kp, fs, ServiceOptions{
		ServerName: "t", ImplementationName: "t", SkinDomains: []string{"localhost"},
	})
	skin := makeSkinPNGColor(t, 64, 64, color.RGBA{255, 0, 0, 255})
	// 上限 = 1.5 张纹理：第一张通过，第二张不同纹理超限被拒
	svc.SetQuota(Quota{StorageBytes: int64(len(skin)) * 3 / 2})

	if err := svc.UploadSkin(ctx, "Alice", "default", bytes.NewReader(skin)); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	other := makeSkinPNGColor(t, 64, 64, color.RGBA{0, 255, 0, 255})
	if err := svc.UploadSkin(ctx, "Alice", "default", bytes.NewReader(other)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("second upload err=%v, want ErrQuotaExceeded", err)
	}
	// 同一纹理重传（sha256 去重，不新增用量）仍成功
	if err := svc.UploadSkin(ctx, "Alice", "default", bytes.NewReader(skin)); err != nil {
		t.Fatalf("re-upload same texture: %v", err)
	}
	// 超限后皮肤未被更换（仍是第一张的 key）
	pd, err := store.ProfileByName(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if pd.SkinKey == "" {
		t.Fatal("skin key should remain from first upload")
	}
}

func TestQuotaTierOverride(t *testing.T) {
	q := Quota{StorageBytes: 100, Tiers: map[string]int64{"pro": 999}}
	if got := q.LimitFor("free"); got != 100 {
		t.Fatalf("free limit=%d", got)
	}
	if got := q.LimitFor("pro"); got != 999 {
		t.Fatalf("pro limit=%d", got)
	}
}
