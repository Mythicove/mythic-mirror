package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/account"
	capimpl "github.com/Mythicove/mythic-mirror/internal/captcha"
	"github.com/Mythicove/mythic-mirror/internal/config"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
	"github.com/Mythicove/mythic-mirror/internal/i18n"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/maillog"
	"github.com/Mythicove/mythic-mirror/internal/memcache"
	oauthimpl "github.com/Mythicove/mythic-mirror/internal/oauth"
	"github.com/Mythicove/mythic-mirror/internal/pgstore"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
	"github.com/Mythicove/shared/captcha"
	"github.com/Mythicove/shared/oauth"
)

func main() {
	cfg := config.Load()

	kp, err := keys.LoadOrGenerate(cfg.KeyPath)
	if err != nil {
		log.Fatalf("load keys: %v", err)
	}

	files, err := filestorage.New(cfg.TexturesDir, cfg.TexturesBaseURL)
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

	acct := account.NewService(store, maillog.Mailer{}, cfg.BaseURL)
	acct.SetLocalizer(loc, cfg.Locale)
	acct.Configure(account.Options{
		InviteOnly:           cfg.InviteOnly,
		DisableRegistration:  cfg.DisableRegistration,
		EmailDomainWhitelist: cfg.EmailDomainWhitelist,
		Captcha:              buildCaptcha(cfg),
		OAuthProviders:       buildOAuthProviders(cfg),
	})

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpapi.NewRouter(svc, acct),
	}

	log.Printf("mythic-mirror listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
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
