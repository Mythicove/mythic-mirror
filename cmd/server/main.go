package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mythicove/mythic-mirror/internal/account"
	capimpl "github.com/Mythicove/mythic-mirror/internal/captcha"
	cdnimpl "github.com/Mythicove/mythic-mirror/internal/cdn"
	"github.com/Mythicove/mythic-mirror/internal/config"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
	"github.com/Mythicove/mythic-mirror/internal/i18n"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/maillog"
	"github.com/Mythicove/mythic-mirror/internal/memcache"
	oauthimpl "github.com/Mythicove/mythic-mirror/internal/oauth"
	"github.com/Mythicove/mythic-mirror/internal/pgstore"
	"github.com/Mythicove/mythic-mirror/internal/resendmail"
	"github.com/Mythicove/mythic-mirror/internal/s3storage"
	"github.com/Mythicove/mythic-mirror/internal/smtpmail"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/cdn"
	"github.com/Mythicove/shared/mailer"
	"github.com/Mythicove/shared/oauth"
	"github.com/Mythicove/shared/storage"
)

func main() {
	cfg := config.Load()

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

	svc := yggdrasil.NewService(store, kp, files, yggdrasil.ServiceOptions{
		ServerName:         cfg.ServerName,
		ImplementationName: cfg.ImplementationName,
		SkinDomains:        cfg.SkinDomains,
		TexturesBaseURL:    cfg.TexturesBaseURL,
	})
	svc.SetCache(memcache.New())
	svc.SetCDN(buildCDN(cfg))

	acct := account.NewService(store, buildMailer(cfg), cfg.BaseURL)
	acct.SetLocalizer(loc, cfg.Locale)
	acct.Configure(account.Options{
		InviteOnly:           cfg.InviteOnly,
		DisableRegistration:  cfg.DisableRegistration,
		EmailDomainWhitelist: cfg.EmailDomainWhitelist,
		Captcha:              buildCaptcha(cfg),
		OAuthProviders:       buildOAuthProviders(cfg),
	})

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.NewRouter(svc, acct, httpapi.Options{
			RateLimitRPS:   cfg.RateLimitRPS,
			RateLimitBurst: cfg.RateLimitBurst,
		}),
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

func buildOAuthProviders(cfg *config.Config) map[string]oauth.Provider {
	providers := map[string]oauth.Provider{}
	if cfg.OAuthGitHubClientID != "" {
		providers["github"] = oauthimpl.NewGitHub(cfg.OAuthGitHubClientID, cfg.OAuthGitHubClientSecret, cfg.BaseURL+"/oauth/github/callback")
	}
	return providers
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
