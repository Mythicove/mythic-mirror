package yggdrasil

import (
	"context"
	"encoding/json"
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

// BindingRequiredCode 是「需先绑定第三方账号」的稳定错误码（前端据此跳绑定引导，不做文案匹配）。
const BindingRequiredCode = "binding_required"

// PasswordLoginDisabledCode 是「主密码登录已关闭」的稳定错误码（前端据此引导第三方登录）。
const PasswordLoginDisabledCode = "password_login_disabled"

// CheckBinding 在管理员要求绑定第三方时，拒绝尚未绑定的账号。
// 皮肤/皮肤库/启动器登录/SSO 共用这一条规则；网站登录、绑定流程本身与账号管理不受限，
// 否则用户会被锁在"必须先绑定才能进入绑定页"的死循环里。
func CheckBinding(ctx context.Context, store Store, policy AuthPolicySource, userID string) error {
	if policy == nil {
		return nil
	}
	p, err := policy.AuthPolicy(ctx)
	if err != nil {
		return err
	}
	if !p.RequireBinding {
		return nil
	}
	ids, err := store.Identities(ctx, userID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrBindingRequired
	}
	return nil
}

// RequireBinding 把 CheckBinding 包成中间件，挂在已认证的路由上（须先经 Auth）。
func RequireBinding(store Store, policy AuthPolicySource) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := SessionFromContext(r.Context())
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err := CheckBinding(r.Context(), store, policy, sess.UserID); err != nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": BindingRequiredCode})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CheckPasswordLogin 实施主密码策略：开关关闭时，只对「尚未绑定第三方」的历史账号放行（宽限），
// 绑定过的账号一律拒绝。于是纯密码账号归零的那一刻主密码登录自然完全失效——
// 不需要第二个开关，也不需要在登录热路径上统计全站人数（这里只读该账号自己的绑定）。
// 网站登录、IdP 登录表单与密码重置共用这一条规则（只此一份实现）。
func CheckPasswordLogin(ctx context.Context, store Store, policy AuthPolicySource, userID string) error {
	if policy == nil {
		return nil
	}
	p, err := policy.AuthPolicy(ctx)
	if err != nil {
		return err
	}
	if p.PasswordLogin {
		return nil
	}
	ids, err := store.Identities(ctx, userID)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		return ErrPasswordLoginDisabled
	}
	return nil
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
