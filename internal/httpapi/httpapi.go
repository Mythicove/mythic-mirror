// Package httpapi 组装 chi 路由与中间件。
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/account"
	"github.com/Mythicove/mythic-mirror/internal/admin"
	"github.com/Mythicove/mythic-mirror/internal/oidc"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/clientip"
	"github.com/Mythicove/shared/ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Options 是路由配置。
type Options struct {
	RateLimitRPS   float64
	RateLimitBurst int
	ClientIP       *clientip.Resolver // 可信代理解析客户端 IP；nil 时用默认配置（头序默认 + 信任私网/环回代理）
	CORSOrigins    []string           // 允许的跨域来源；空则回退 "*"（仅建议开发环境）
}

// defaultResolver 是 Options.ClientIP 缺省时的解析器（默认配置不可能构造失败）。
var defaultResolver = func() *clientip.Resolver {
	r, _ := clientip.New(clientip.Options{})
	return r
}()

// NewRouter 组装 HTTP 路由与中间件。
func NewRouter(svc *yggdrasil.Service, acct *account.Service, adminSvc *admin.Service, oidcSvc *oidc.Provider, opts Options) http.Handler {
	resolver := opts.ClientIP
	if resolver == nil {
		resolver = defaultResolver
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(clientip.Middleware(resolver))
	r.Use(middleware.Recoverer)
	r.Use(slogLogger())
	r.Use(metricsMW())
	origins := opts.CORSOrigins
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(securityHeaders())
	r.Use(yggdrasil.BodyLimit(10 << 20))
	if opts.RateLimitRPS > 0 {
		r.Use(yggdrasil.RateLimit(ratelimit.NewMemory(), opts.RateLimitBurst, time.Second))
	}

	r.Get("/healthz", handleHealth)
	r.Handle("/metrics", promhttp.Handler())
	svc.Register(r)
	acct.RegisterRoutes(r)
	if adminSvc != nil {
		adminSvc.RegisterRoutes(r)
	}
	if oidcSvc != nil {
		oidcSvc.RegisterRoutes(r)
	}
	return r
}

// securityHeaders 设置基础安全响应头。
// CSP 允许内联 style：OIDC 登录/授权页使用内嵌 <style>；API 本体只返回 JSON/图片。
func securityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; style-src 'unsafe-inline'")
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func slogLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			slog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
