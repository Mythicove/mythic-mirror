// Package filestorage 提供本地磁盘存储，实现 shared/storage.Storage。
package filestorage

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Mythicove/shared/storage"
)

// Storage 是本地磁盘存储。
type Storage struct {
	dir     string
	baseURL string
}

// New 构造本地存储；baseURL 用于 PresignGet 生成直出 URL。
func New(dir, baseURL string) (*Storage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Storage{dir: dir, baseURL: baseURL}, nil
}

// SetBaseURL 更新直出 URL 的基址（测试/动态部署用）。
func (s *Storage) SetBaseURL(baseURL string) { s.baseURL = baseURL }

// Put 写入对象并返回存储键。
func (s *Storage) Put(_ context.Context, o storage.Object) (string, error) {
	path := filepath.Join(s.dir, o.Key)
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, o.Body); err != nil {
		return "", err
	}
	return o.Key, nil
}

// Get 读取对象。
func (s *Storage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.dir, key))
}

// Delete 删除对象。
func (s *Storage) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(s.dir, key))
}

// PresignGet 返回直出 URL（形如 {baseURL}/textures/{key}）。
func (s *Storage) PresignGet(_ context.Context, key string) (*url.URL, error) {
	return url.Parse(s.baseURL + "/textures/" + key)
}
