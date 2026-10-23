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
}

// NewRouter 组装 HTTP 路由与中间件。
func NewRouter(svc *yggdrasil.Service, acct *account.Service, adminSvc *admin.Service, oidcSvc *oidc.Provider, opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(slogLogger())
	r.Use(metricsMW())
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
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
