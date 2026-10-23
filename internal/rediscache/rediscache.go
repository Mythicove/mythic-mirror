// Package rediscache 提供 Redis 缓存的 cache.Cache 实现（高档，多实例共享）。
package rediscache

import (
	"context"
	"time"

	"github.com/Mythicove/shared/cache"
	"github.com/redis/go-redis/v9"
)

// Cache 是 Redis 缓存。
type Cache struct {
	client *redis.Client
}

var _ cache.Cache = (*Cache)(nil)

// New 构造 Redis 缓存并 ping。
func New(ctx context.Context, addr string) (*Cache, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Cache{client: client}, nil
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

func (c *Cache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	return c.client.Set(ctx, key, val, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
