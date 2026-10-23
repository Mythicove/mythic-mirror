// Package redislimit 提供 Redis 固定窗口限流（ratelimit.Limiter 的高档实现），
// 多副本共享计数；单副本/无 Redis 时退回 ratelimit.Memory。
package redislimit

import (
	"context"
	"strconv"
	"time"

	"github.com/Mythicove/shared/ratelimit"
	"github.com/redis/go-redis/v9"
)

// incrScript 原子执行 INCR + 首次 PEXPIRE，返回窗口内计数。
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// Limiter 是 Redis 固定窗口限流器；ns 隔离不同用途（global/auth）。
type Limiter struct {
	client redis.UniversalClient
	ns     string
}

var _ ratelimit.Limiter = (*Limiter)(nil)

// New 连接 Redis 并 ping；失败返回错误（调用方决定回退）。
func New(ctx context.Context, addr, ns string) (*Limiter, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Limiter{client: client, ns: ns}, nil
}

// NewWithClient 复用已有连接。
func NewWithClient(client redis.UniversalClient, ns string) *Limiter {
	return &Limiter{client: client, ns: ns}
}

// Allow 判断 key 在 window 内的请求数是否超过 limit。
// Redis 故障时放行（fail-open）：限流是防滥用手段，不应成为可用性单点。
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	n, err := incrScript.Run(ctx, l.client,
		[]string{"rl:" + l.ns + ":" + key},
		window.Milliseconds(),
	).Int64()
	if err != nil {
		return true, nil
	}
	return n <= int64(limit), nil
}

// Count 返回 key 当前窗口计数（测试/诊断用）。
func (l *Limiter) Count(ctx context.Context, key string) (int, error) {
	v, err := l.client.Get(ctx, "rl:"+l.ns+":"+key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}
