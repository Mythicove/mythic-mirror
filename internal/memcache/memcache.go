// Package memcache 提供进程内缓存的 cache.Cache 实现（低档）。
package memcache

import (
	"context"
	"sync"
	"time"

	"github.com/Mythicove/shared/cache"
)

type entry struct {
	val    []byte
	expire time.Time
}

// Cache 是带 TTL 的进程内缓存。
type Cache struct {
	mu sync.Mutex
	m  map[string]entry
}

var _ cache.Cache = (*Cache)(nil)

// New 构造缓存。
func New() *Cache {
	return &Cache{m: map[string]entry{}}
}

func (c *Cache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, false, nil
	}
	if !e.expire.IsZero() && time.Now().After(e.expire) {
		delete(c.m, key)
		return nil, false, nil
	}
	return e.val, true, nil
}

func (c *Cache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// ttl <= 0 表示不过期（与 shared/cache 契约、Redis SET 不带 TTL 的语义一致）。
	var expire time.Time
	if ttl > 0 {
		expire = time.Now().Add(ttl)
	}
	c.m[key] = entry{val: val, expire: expire}
	return nil
}

func (c *Cache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}
