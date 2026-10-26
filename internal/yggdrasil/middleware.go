package yggdrasil

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Mythicove/shared/ratelimit"
)

// SessionCookieName 是 web 前端的 httpOnly 会话 cookie 名（浏览器端 Bearer 替代）。
// 安全模型：cookie 设 SameSite=Lax（跨站请求不携带 → CSRF 被阻断），且所有变更端点
// 只接受 JSON body（跨站表单无法伪造 Content-Type）；启动器/游戏客户端继续用 Authorization 头。
const SessionCookieName = "mythic_session"

// BearerToken 提取 Authorization: Bearer 头；缺失时回退读 web 会话 cookie。
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		return c.Value
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

// adminActorKey 是管理操作者标识的 context 键（审计用）。
type adminActorKey struct{}

// AdminRole 校验管理员身份：静态 token（救援通道）**或** role=admin 的会话
// （Authorization: Bearer 或 web 的 httpOnly 会话 cookie）。通过后把操作者写入
// context，用 AdminActor 读取（"token" 或 "user:<id>"）。
// 全站后台管理端点统一走它，避免"只认静态 token"与"认会话"两套语义并存。
func AdminRole(store Store, adminToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if adminToken != "" && token == adminToken {
				next.ServeHTTP(w, r.WithContext(WithAdminActor(r.Context(), "token")))
				return
			}
			if sess, err := store.Validate(r.Context(), token, ""); err == nil {
				if role, err := store.UserRole(r.Context(), sess.UserID); err == nil && role == "admin" {
					ctx := WithAdminActor(r.Context(), "user:"+sess.UserID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

// WithAdminActor 把管理操作者标识写入 context。
func WithAdminActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, adminActorKey{}, actor)
}

// AdminActor 读取管理操作者标识；未经 AdminRole 时返回 "unknown"。
func AdminActor(ctx context.Context) string {
	if v, ok := ctx.Value(adminActorKey{}).(string); ok {
		return v
	}
	return "unknown"
}

// ClientIP 提取客户端 IP（去掉端口），作为限流键。
// 反代场景 clientip.Middleware（可信代理感知）已把 RemoteAddr 还原为真实客户端 IP。
func ClientIP(r *http.Request) string {
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// RateLimit 返回基于 IP 的固定窗口限流中间件（实现见 shared/ratelimit）。
// name 隔离不同挂载点的计数（如 "global" 与 "auth"），避免共享 Redis 时相互干扰。
func RateLimit(l ratelimit.Limiter, name string, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, err := l.Allow(r.Context(), name+":"+ClientIP(r), limit, window)
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
