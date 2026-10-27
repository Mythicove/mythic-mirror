package admin

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/go-chi/chi/v5"
)

func testPNG() []byte {
	return append(append([]byte{}, pngSignature...), []byte("fake-png-body")...)
}

func newBrandService(t *testing.T) *Service {
	t.Helper()
	fs, err := filestorage.New(t.TempDir(), "http://files.local")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(yggdrasil.NewMemoryStore(), "admintoken", nil)
	svc.SetStorage(fs)
	return svc
}

func TestBrandLogoLifecycle(t *testing.T) {
	ctx := context.Background()
	svc := newBrandService(t)

	// 未自定义：Brand 零值、BrandLogo 404 语义
	if c, _ := svc.Brand(ctx); c.Light || c.Dark || c.Version != 0 {
		t.Fatalf("expected zero brand, got %+v", c)
	}
	if _, err := svc.BrandLogo(ctx, BrandLight); err != io.EOF {
		t.Fatalf("expected io.EOF before upload, got %v", err)
	}

	png := testPNG()
	if err := svc.SetBrandLogo(ctx, BrandLight, png); err != nil {
		t.Fatal(err)
	}
	c, _ := svc.Brand(ctx)
	if !c.Light || c.Dark || c.Version != 1 {
		t.Fatalf("after set light: %+v", c)
	}
	got, err := svc.BrandLogo(ctx, BrandLight)
	if err != nil || !bytes.Equal(got, png) {
		t.Fatalf("roundtrip mismatch: %v", err)
	}

	// 非法输入拒绝
	if err := svc.SetBrandLogo(ctx, BrandDark, []byte("not-a-png")); err != ErrInvalidBrandLogo {
		t.Fatalf("expected ErrInvalidBrandLogo, got %v", err)
	}
	if err := svc.SetBrandLogo(ctx, BrandDark, bytes.Repeat([]byte{1}, MaxBrandLogoBytes+1)); err != ErrInvalidBrandLogo {
		t.Fatalf("expected size reject, got %v", err)
	}

	// 删除回落 + 版本继续自增
	if err := svc.DeleteBrandLogo(ctx, BrandLight); err != nil {
		t.Fatal(err)
	}
	c, _ = svc.Brand(ctx)
	if c.Light || c.Version != 2 {
		t.Fatalf("after delete: %+v", c)
	}
	if _, err := svc.BrandLogo(ctx, BrandLight); err != io.EOF {
		t.Fatalf("expected io.EOF after delete, got %v", err)
	}

	if _, err := ParseBrandVariant("purple"); err != ErrInvalidBrandVariant {
		t.Fatalf("expected ErrInvalidBrandVariant, got %v", err)
	}
}

func TestBrandHTTP(t *testing.T) {
	svc := newBrandService(t)
	r := chi.NewRouter()
	svc.RegisterRoutes(r)

	// 公开：未自定义时 logo 404、brand 零值
	rr := httptest.NewRecorder()
	get(r, rr, "/api/brand/logo/light")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("public logo before upload: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	get(r, rr, "/api/brand")
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"light":false`)) {
		t.Fatalf("public brand: %d %s", rr.Code, rr.Body.String())
	}

	// 管理：无凭据 401/403，带 token 204 后公开可读
	put := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/brand/logo/light", bytes.NewReader(testPNG()))
	r.ServeHTTP(put, req)
	if put.Code == http.StatusNoContent {
		t.Fatal("admin PUT must require auth")
	}
	put = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/admin/brand/logo/light", bytes.NewReader(testPNG()))
	req.Header.Set("Authorization", "Bearer admintoken")
	r.ServeHTTP(put, req)
	if put.Code != http.StatusNoContent {
		t.Fatalf("admin PUT with token: %d %s", put.Code, put.Body.String())
	}
	rr = httptest.NewRecorder()
	get(r, rr, "/api/brand/logo/light")
	if rr.Code != http.StatusOK || rr.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("public logo after upload: %d ct=%q", rr.Code, rr.Header().Get("Content-Type"))
	}
}

func get(r http.Handler, rr *httptest.ResponseRecorder, path string) {
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
}
