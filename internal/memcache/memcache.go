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
	if time.Now().After(e.expire) {
		delete(c.m, key)
		return nil, false, nil
	}
	return e.val, true, nil
}

func (c *Cache) Set(_ context.Context, key string, val []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = entry{val: val, expire: time.Now().Add(ttl)}
	return nil
}

func (c *Cache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}
