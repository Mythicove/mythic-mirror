package filestorage

import (
	"context"
	"testing"
)

// 直出 URL 契约：baseURL 是站点基址，/textures/ 段由实现持有（对应网关 /textures/* 路由）。
// 与 s3storage 的同名测试一起锁定两种驱动的形状一致。
func TestPresignGetTexturePrefix(t *testing.T) {
	s, err := New(t.TempDir(), "http://localhost:8080")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	u, err := s.PresignGet(context.Background(), "ab/cd.png")
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if want := "http://localhost:8080/textures/ab/cd.png"; u.String() != want {
		t.Fatalf("PresignGet = %q, want %q", u, want)
	}
}
