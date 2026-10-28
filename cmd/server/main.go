package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
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
	"github.com/Mythicove/mythic-mirror/internal/configseed"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
	"github.com/Mythicove/mythic-mirror/internal/i18n"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/maillog"
	"github.com/Mythicove/mythic-mirror/internal/memcache"
	"github.com/Mythicove/mythic-mirror/internal/migrate"
	"github.com/Mythicove/mythic-mirror/internal/mojang"
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
	oauthProviders := buildOAuthProviders(cfg)
	// 认证策略来源（主密码开关 / 强制绑定第三方 / 玩家数上限）：yggdrasil、account、oidc
	// 三处都从这里读同一份策略，避免各写一套判定。
	adminSvc := admin.NewService(store, cfg.AdminToken, providerLabels(oauthProviders))
	// 品牌图标（后台上传的明暗 logo）持久化到对象存储
	adminSvc.SetStorage(files)

	svc := yggdrasil.NewService(store, kp, files, yggdrasil.ServiceOptions{
		ServerName:         cfg.ServerName,
		ImplementationName: cfg.ImplementationName,
		SkinDomains:        cfg.SkinDomains,
		TexturesBaseURL:    cfg.TexturesBaseURL,
	})
	svc.SetCache(cacheImpl)
	svc.SetCDN(buildCDN(cfg))
	svc.SetLimiter(limiter)
	svc.SetPolicy(adminSvc)
	quota := buildQuota(cfg)
	svc.SetQuota(quota)
	// Mojang 解析器：正版验证（POST /mojang/verify）与正版皮肤/披风回退都依赖它。
	// 不设则 resolver==nil，这两条路径恒失败（此前一直没接线，端点在生产是死的）。
	// 惰性调用：只在命中上述路径时才出网到 api.mojang.com。
	svc.SetResolver(mojang.New())

	acct := account.NewService(store, buildMailer(cfg), cfg.BaseURL, cacheImpl)
	acct.SetLocalizer(loc, cfg.Locale)
	acct.SetQuota(quota)
	acct.SetPolicy(adminSvc)
	acct.SetPolicySource(adminSvc)
	acct.Configure(account.Options{
		Captcha:        buildCaptcha(cfg),
		CaptchaDriver:  cfg.CaptchaDriver,
		CaptchaSiteKey: cfg.CaptchaSiteKey,
		CaptchaCapURL:  cfg.CaptchaCapURL,
		Limiter:        limiter,
		OAuthProviders: oauthProviders,
		AdminToken:     cfg.AdminToken,
		MojangOAuth:    buildMojangOAuth(cfg),
		WebURL:         cfg.WebURL,
		SkinReview:     cfg.SkinReview,
	})
	// 一次性把废弃的注册/验证码范围 env 播种进后台策略（settings KV），之后 env 失效。
	seedRegistrationPolicy(store, cfg)

	// OIDC 会话存储：有 Postgres 落库（多副本/重启安全），否则进程内存（开发）。
	var oidcPersister oidc.SessionPersister
	if pg, ok := store.(*pgstore.Store); ok {
		oidcPersister = pg.OIDC()
	}
	oidcSvc := oidc.NewProvider(store, kp, cfg.BaseURL, oidcPersister)
	oidcSvc.SetAdminToken(cfg.AdminToken)
	// SetLimiter 必须在 RegisterRoutes 之前（路由注册时捕获限流器）
	oidcSvc.SetLimiter(limiter)
	oidcSvc.SetPolicy(adminSvc)
	// 登录/同意 UI 由前端承载；未配置时授权端点返回 500 而非回退后端 HTML
	oidcSvc.SetWebURL(cfg.WebURL)
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
	base := cfg.TexturesBaseURL
	if base == "" {
		base = cfg.BaseURL // 本地驱动必须有公开基址；S3 留空走预签名
	}
	return filestorage.New(cfg.TexturesDir, base)
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
		if u, err := url.Parse(cfg.CDNWebhookURL); err != nil || u.Scheme == "" || u.Host == "" {
			log.Printf("warn: invalid MIRROR_CDN_WEBHOOK_URL, CDN webhook disabled")
			return nil
		}
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
	case "cap":
		if cfg.CaptchaCapURL == "" || cfg.CaptchaSiteKey == "" || cfg.CaptchaSecret == "" {
			log.Printf("captcha driver cap requires MIRROR_CAPTCHA_CAP_URL, MIRROR_CAPTCHA_SITE_KEY and MIRROR_CAPTCHA_SECRET; captcha disabled")
			return nil
		}
		return capimpl.NewCap(cfg.CaptchaCapURL, cfg.CaptchaSiteKey, cfg.CaptchaSecret)
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

// seedRegistrationPolicy 把废弃的注册/验证码范围环境变量一次性播种进后台策略
// （settings KV）。播种完成后这些 env 不再生效；仍被设置时启动打 WARN 提醒改到后台。
func seedRegistrationPolicy(store yggdrasil.Store, cfg *config.Config) {
	envs := []string{
		"MIRROR_DISABLE_REGISTRATION", "MIRROR_INVITE_ONLY",
		"MIRROR_EMAIL_DOMAIN_WHITELIST", "MIRROR_CAPTCHA_SCOPES",
	}
	item := configseed.Item{
		Key:  admin.SettingRegistration,
		Envs: envs,
		Build: func(getenv func(string) string) ([]byte, bool) {
			anySet := false
			for _, e := range envs {
				if getenv(e) != "" {
					anySet = true
				}
			}
			if !anySet {
				return nil, false // 无旧值可播种：保持键缺失=读侧默认
			}
			p := account.RegistrationPolicy{
				DisableRegistration:  getenv("MIRROR_DISABLE_REGISTRATION") == "true",
				InviteOnly:           getenv("MIRROR_INVITE_ONLY") == "true",
				EmailDomainWhitelist: splitCSV(getenv("MIRROR_EMAIL_DOMAIN_WHITELIST")),
				CaptchaScopes:        splitCSV(getenv("MIRROR_CAPTCHA_SCOPES")),
			}
			b, err := json.Marshal(p)
			return b, err == nil
		},
	}
	run := func(ctx context.Context) error {
		seeded, stale, err := configseed.Run(ctx, store, configseed.Version, []configseed.Item{item}, os.Getenv)
		for _, k := range seeded {
			log.Printf("configseed: seeded %q from legacy environment variables", k)
		}
		for _, e := range stale {
			log.Printf("configseed: WARNING %s is deprecated and ignored; manage it in the admin panel", e)
		}
		return err
	}
	var err error
	if cfg.DatabaseURL != "" {
		err = migrate.WithLock(cfg.DatabaseURL, run) // 与迁移同锁，多副本安全
	} else {
		err = run(context.Background())
	}
	if err != nil {
		log.Printf("configseed: %v", err)
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
