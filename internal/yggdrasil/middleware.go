package yggdrasil

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Mythicove/shared/ratelimit"
)

// BearerToken 提取 Authorization: Bearer 头。
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// Auth 校验 Bearer token 并把会话注入 context。
func Auth(store Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			sess, err := store.Validate(r.Context(), token, "")
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), sess)))
		})
	}
}

// Admin 校验管理员 token（空 token 表示禁用）。
func Admin(adminToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if adminToken == "" || BearerToken(r) != adminToken {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP 提取客户端 IP（去掉端口），作为限流键。
// 反代场景 chi 的 middleware.RealIP 已把 RemoteAddr 还原为真实 IP。
func ClientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// RateLimit 返回基于 IP 的固定窗口限流中间件（实现见 shared/ratelimit）。
func RateLimit(l ratelimit.Limiter, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, err := l.Allow(r.Context(), ClientIP(r), limit, window)
			if err == nil && !ok {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit 限制请求体大小。
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
