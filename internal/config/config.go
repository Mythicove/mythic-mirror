// Package config 从环境变量加载配置。
package config

import (
	"os"
	"strings"
)

// Config 是服务运行所需的配置。
type Config struct {
	ListenAddr         string
	ServerName         string
	ImplementationName string
	KeyPath            string
	TexturesDir        string
	DatabaseURL        string
	SkinDomains        []string
	TexturesBaseURL    string
	BaseURL            string
	Locale             string

	CaptchaDriver         string
	CaptchaSecret         string
	OAuthGitHubClientID     string
	OAuthGitHubClientSecret string
	InviteOnly            bool
	DisableRegistration   bool
	EmailDomainWhitelist  []string

	StorageDriver string
	S3Endpoint    string
	S3Region      string
	S3Bucket      string
	S3AccessKey   string
	S3SecretKey   string

	MailDriver   string
	SMTPAddr     string
	SMTPFrom     string
	SMTPUser     string
	SMTPPass     string
	ResendAPIKey string
	ResendFrom   string

	CDNDriver        string
	CloudflareZoneID string
	CloudflareAPIKey string
	BunnyAPIKey      string
	CDNWebhookURL    string
}

// Load 从环境变量读取配置，缺省值见 .env.example。
func Load() *Config {
	return &Config{
		ListenAddr:         getenv("MIRROR_LISTEN_ADDR", ":8080"),
		ServerName:         getenv("MIRROR_SERVER_NAME", "Mythic Mirror"),
		ImplementationName: getenv("MIRROR_IMPL_NAME", "mythic-mirror"),
		KeyPath:            getenv("MIRROR_KEY_PATH", "data/private.pem"),
		TexturesDir:        getenv("MIRROR_TEXTURES_DIR", "data/textures"),
		DatabaseURL:        getenv("MIRROR_DATABASE_URL", ""),
		SkinDomains:        split(getenv("MIRROR_SKIN_DOMAINS", "localhost")),
		TexturesBaseURL:    getenv("MIRROR_TEXTURES_BASE_URL", "http://localhost:8080"),
		BaseURL:            getenv("MIRROR_BASE_URL", "http://localhost:8080"),
		Locale:             getenv("MIRROR_LOCALE", "en-US"),

		CaptchaDriver:          getenv("MIRROR_CAPTCHA_DRIVER", ""),
		CaptchaSecret:          getenv("MIRROR_CAPTCHA_SECRET", ""),
		OAuthGitHubClientID:     getenv("MIRROR_OAUTH_GITHUB_CLIENT_ID", ""),
		OAuthGitHubClientSecret: getenv("MIRROR_OAUTH_GITHUB_CLIENT_SECRET", ""),
		InviteOnly:             getenvBool("MIRROR_INVITE_ONLY"),
		DisableRegistration:    getenvBool("MIRROR_DISABLE_REGISTRATION"),
		EmailDomainWhitelist:   split(getenv("MIRROR_EMAIL_DOMAIN_WHITELIST", "")),

		StorageDriver: getenv("MIRROR_STORAGE_DRIVER", "local"),
		S3Endpoint:    getenv("MIRROR_S3_ENDPOINT", ""),
		S3Region:      getenv("MIRROR_S3_REGION", "auto"),
		S3Bucket:      getenv("MIRROR_S3_BUCKET", ""),
		S3AccessKey:   getenv("MIRROR_S3_ACCESS_KEY", ""),
		S3SecretKey:   getenv("MIRROR_S3_SECRET_KEY", ""),

		MailDriver:   getenv("MIRROR_MAIL_DRIVER", "log"),
		SMTPAddr:     getenv("MIRROR_SMTP_ADDR", ""),
		SMTPFrom:     getenv("MIRROR_SMTP_FROM", ""),
		SMTPUser:     getenv("MIRROR_SMTP_USER", ""),
		SMTPPass:     getenv("MIRROR_SMTP_PASS", ""),
		ResendAPIKey: getenv("MIRROR_RESEND_API_KEY", ""),
		ResendFrom:   getenv("MIRROR_RESEND_FROM", ""),

		CDNDriver:        getenv("MIRROR_CDN_DRIVER", ""),
		CloudflareZoneID: getenv("MIRROR_CLOUDFLARE_ZONE_ID", ""),
		CloudflareAPIKey: getenv("MIRROR_CLOUDFLARE_API_KEY", ""),
		BunnyAPIKey:      getenv("MIRROR_BUNNY_API_KEY", ""),
		CDNWebhookURL:    getenv("MIRROR_CDN_WEBHOOK_URL", ""),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
