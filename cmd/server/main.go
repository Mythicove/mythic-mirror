package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/account"
	"github.com/Mythicove/mythic-mirror/internal/admin"
	capimpl "github.com/Mythicove/mythic-mirror/internal/captcha"
	cdnimpl "github.com/Mythicove/mythic-mirror/internal/cdn"
	"github.com/Mythicove/mythic-mirror/internal/config"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
	"github.com/Mythicove/mythic-mirror/internal/i18n"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/maillog"
	"github.com/Mythicove/mythic-mirror/internal/memcache"
	"github.com/Mythicove/mythic-mirror/internal/migrate"
	oauthimpl "github.com/Mythicove/mythic-mirror/internal/oauth"
	"github.com/Mythicove/mythic-mirror/internal/oidc"
	"github.com/Mythicove/mythic-mirror/internal/pgstore"
	"github.com/Mythicove/mythic-mirror/internal/rediscache"
	"github.com/Mythicove/mythic-mirror/internal/redislimit"
	"github.com/Mythicove/mythic-mirror/internal/resendmail"
	"github.com/Mythicove/mythic-mirror/internal/s3storage"
	"github.com/Mythicove/mythic-mirror/internal/smtpmail"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/cache"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/cdn"
	"github.com/Mythicove/shared/clientip"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/oauth"
	"github.com/Mythicove/shared/ratelimit"
	"github.com/Mythicove/shared/storage"
)

func main() {
	cfg := config.Load()

	if cfg.DatabaseURL != "" {
		if err := migrate.Run(cfg.DatabaseURL); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	kp, err := keys.LoadOrGenerate(cfg.KeyPath)
	if err != nil {
		log.Fatalf("load keys: %v", err)
	}

	files, err := buildStorage(context.Background(), cfg)
	if err != nil {
		log.Fatalf("init storage: %v", err)
	}

	store, closeStore, err := newStore(cfg)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	if closeStore != nil {
		defer closeStore()
	}
	loc, err := i18n.New()
	if err != nil {
		log.Fatalf("init i18n: %v", err)
	}

	cacheImpl := buildCache(context.Background(), cfg)
	limiter := buildLimiter(context.Background(), cfg)
	svc := yggdrasil.NewService(store, kp, files, yggdrasil.ServiceOptions{
		ServerName:         cfg.ServerName,
		ImplementationName: cfg.ImplementationName,
		SkinDomains:        cfg.SkinDomains,
		TexturesBaseURL:    cfg.TexturesBaseURL,
	})
	svc.SetCache(cacheImpl)
	svc.SetCDN(buildCDN(cfg))
	svc.SetLimiter(limiter)
	quota := buildQuota(cfg)
	svc.SetQuota(quota)

	acct := account.NewService(store, buildMailer(cfg), cfg.BaseURL, cacheImpl)
	acct.SetLocalizer(loc, cfg.Locale)
	acct.SetQuota(quota)
	oauthProviders := buildOAuthProviders(cfg)
	acct.Configure(account.Options{
		InviteOnly:           cfg.InviteOnly,
		DisableRegistration:  cfg.DisableRegistration,
		EmailDomainWhitelist: cfg.EmailDomainWhitelist,
		Captcha:              buildCaptcha(cfg),
		OAuthProviders:       oauthProviders,
		AdminToken:           cfg.AdminToken,
		MojangOAuth:          buildMojangOAuth(cfg),
		WebURL:               cfg.WebURL,
		SkinReview:           cfg.SkinReview,
	})

	adminSvc := admin.NewService(store, cfg.AdminToken, providerLabels(oauthProviders))

	// OIDC 会话存储：有 Postgres 落库（多副本/重启安全），否则进程内存（开发）。
	var oidcPersister oidc.SessionPersister
	if pg, ok := store.(*pgstore.Store); ok {
		oidcPersister = pg.OIDC()
	}
	oidcSvc := oidc.NewProvider(store, kp, cfg.BaseURL, oidcPersister)
	oidcSvc.SetAdminToken(cfg.AdminToken)
	if err := oidcSvc.ReloadClients(context.Background()); err != nil {
		log.Fatalf("init oidc: %v", err)
	}

	ipResolver, err := clientip.New(clientip.Options{
		Headers:        cfg.ClientIPHeaders,
		TrustedProxies: cfg.TrustedProxies,
		TrustNone:      cfg.ClientIPTrustNone,
	})
	if err != nil {
		log.Fatalf("client ip resolver: %v", err)
	}

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.NewRouter(svc, acct, adminSvc, oidcSvc, httpapi.Options{
			RateLimitRPS:   cfg.RateLimitRPS,
			RateLimitBurst: cfg.RateLimitBurst,
			Limiter:        limiter,
			ClientIP:       ipResolver,
			CORSOrigins:    corsOrigins(cfg),
		}),
		// 上传（皮肤）与下载（纹理）为流式长连接，不设 Read/WriteTimeout，
		// 请求体大小由 BodyLimit 中间件约束；ReadHeaderTimeout 防 Slowloris。
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("mythic-mirror listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// corsOrigins 计算允许的跨域来源：显式配置 > WebURL 派生 > "*"（开发兜底）。
func corsOrigins(cfg *config.Config) []string {
	var out []string
	for _, o := range cfg.CORSOrigins {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	if len(out) == 0 && cfg.WebURL != "" {
		out = append(out, cfg.WebURL)
	}
	return out
}

func buildStorage(ctx context.Context, cfg *config.Config) (storage.Storage, error) {
	if cfg.StorageDriver == "s3" {
		return s3storage.New(ctx, s3storage.Options{
			Endpoint:  cfg.S3Endpoint,
			Region:    cfg.S3Region,
			Bucket:    cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		}, cfg.TexturesBaseURL)
	}
	return filestorage.New(cfg.TexturesDir, cfg.TexturesBaseURL)
}

func buildMailer(cfg *config.Config) mailer.Mailer {
	switch cfg.MailDriver {
	case "smtp":
		return smtpmail.New(cfg.SMTPAddr, cfg.SMTPFrom, cfg.SMTPUser, cfg.SMTPPass)
	case "resend":
		return resendmail.New(cfg.ResendAPIKey, cfg.ResendFrom)
	default:
		return maillog.Mailer{}
	}
}

func buildCDN(cfg *config.Config) cdn.CDN {
	switch cfg.CDNDriver {
	case "cloudflare":
		return cdnimpl.NewCloudflare(cfg.CloudflareZoneID, cfg.CloudflareAPIKey)
	case "bunny":
		return cdnimpl.NewBunny(cfg.BunnyAPIKey)
	case "webhook":
		return cdnimpl.NewWebhook(cfg.CDNWebhookURL)
	default:
		return nil
	}
}

func buildCaptcha(cfg *config.Config) captcha.Captcha {
	switch cfg.CaptchaDriver {
	case "turnstile":
		return capimpl.NewTurnstile(cfg.CaptchaSecret)
	case "recaptcha":
		return capimpl.NewReCaptcha(cfg.CaptchaSecret)
	case "hcaptcha":
		return capimpl.NewHCaptcha(cfg.CaptchaSecret)
	default:
		return nil
	}
}

// buildQuota 构造上传配额（tiers 为 JSON 映射，解析失败仅告警并忽略）。
func buildQuota(cfg *config.Config) yggdrasil.Quota {
	q := yggdrasil.Quota{StorageBytes: cfg.QuotaStorageBytes, Published: cfg.QuotaPublished}
	if cfg.QuotaTiers != "" {
		var tiers map[string]int64
		if err := json.Unmarshal([]byte(cfg.QuotaTiers), &tiers); err != nil {
			log.Printf("invalid MIRROR_QUOTA_TIERS (%v); ignoring tier overrides", err)
		} else {
			q.Tiers = tiers
		}
	}
	return q
}

// buildLimiter 构造限流器：配置 Redis 用 Redis（多副本共享计数），否则/失败退回进程内。
func buildLimiter(ctx context.Context, cfg *config.Config) ratelimit.Limiter {
	if cfg.RedisAddr != "" {
		if l, err := redislimit.New(ctx, cfg.RedisAddr, "mirror"); err == nil {
			return l
		} else {
			log.Printf("redis limiter unavailable (%v); falling back to in-memory", err)
		}
	}
	return ratelimit.NewMemory()
}

func buildCache(ctx context.Context, cfg *config.Config) cache.Cache {
	if cfg.RedisAddr != "" {
		if c, err := rediscache.New(ctx, cfg.RedisAddr); err == nil {
			return c
		} else {
			log.Printf("redis cache unavailable (%v); falling back to in-memory", err)
		}
	}
	return memcache.New()
}

func buildOAuthProviders(cfg *config.Config) map[string]oauth.Provider {
	providers := map[string]oauth.Provider{}
	if cfg.OAuthGitHubClientID != "" {
		providers["github"] = oauthimpl.NewGitHub(cfg.OAuthGitHubClientID, cfg.OAuthGitHubClientSecret, cfg.BaseURL+"/oauth/github/callback")
	}
	return providers
}

func buildMojangOAuth(cfg *config.Config) oauth.Provider {
	if cfg.OAuthMicrosoftClientID != "" {
		return oauthimpl.NewMicrosoft(cfg.OAuthMicrosoftClientID, cfg.OAuthMicrosoftClientSecret, cfg.BaseURL+"/mojang/oauth/callback")
	}
	return nil
}

func providerLabels(providers map[string]oauth.Provider) map[string]string {
	labels := map[string]string{
		"github":    "GitHub",
		"microsoft": "Microsoft",
		"google":    "Google",
		"discord":   "Discord",
		"qq":        "QQ",
		"wechat":    "WeChat",
		"weibo":     "Weibo",
	}
	out := map[string]string{}
	for id := range providers {
		if l, ok := labels[id]; ok {
			out[id] = l
		} else {
			out[id] = id
		}
	}
	return out
}

// newStore 有数据库 URL 用 Postgres，否则退回内存（开发/测试）。
func newStore(cfg *config.Config) (yggdrasil.Store, func(), error) {
	if cfg.DatabaseURL == "" {
		return yggdrasil.NewMemoryStore(), nil, nil
	}
	pg, err := pgstore.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return pg, pg.Close, nil
}
