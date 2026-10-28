package s3storage

import (
	"context"
	"testing"
)

// 直出 URL 契约：baseURL 是站点基址时拼 /textures/ 前缀，与 filestorage 一致；
// 曾出现 s3 驱动漏拼 /textures/ 导致游戏内皮肤 404。
func TestPresignGetTexturePrefix(t *testing.T) {
	s, err := New(context.Background(), Options{
		Endpoint:  "http://127.0.0.1:9000",
		Region:    "us-east-1",
		Bucket:    "textures",
		AccessKey: "test",
		SecretKey: "test",
	}, "http://localhost:8080")
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
