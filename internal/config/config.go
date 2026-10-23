// Package config 从环境变量加载配置（caarlos0/env）。
package config

import "github.com/caarlos0/env/v11"

// Config 是服务运行所需的配置。
type Config struct {
	ListenAddr         string   `env:"MIRROR_LISTEN_ADDR" envDefault:":8080"`
	ServerName         string   `env:"MIRROR_SERVER_NAME" envDefault:"Mythic Mirror"`
	ImplementationName string   `env:"MIRROR_IMPL_NAME" envDefault:"mythic-mirror"`
	KeyPath            string   `env:"MIRROR_KEY_PATH" envDefault:"data/private.pem"`
	TexturesDir        string   `env:"MIRROR_TEXTURES_DIR" envDefault:"data/textures"`
	DatabaseURL        string   `env:"MIRROR_DATABASE_URL"`
	SkinDomains        []string `env:"MIRROR_SKIN_DOMAINS" envSeparator:"," envDefault:"localhost"`
	TexturesBaseURL    string   `env:"MIRROR_TEXTURES_BASE_URL" envDefault:"http://localhost:8080"`
	BaseURL            string   `env:"MIRROR_BASE_URL" envDefault:"http://localhost:8080"`
	WebURL             string   `env:"MIRROR_WEB_URL"`
	CORSOrigins        []string `env:"MIRROR_CORS_ORIGINS" envSeparator:","`
	Locale             string   `env:"MIRROR_LOCALE" envDefault:"en-US"`

	CaptchaDriver              string   `env:"MIRROR_CAPTCHA_DRIVER"`
	CaptchaSecret              string   `env:"MIRROR_CAPTCHA_SECRET"`
	OAuthGitHubClientID        string   `env:"MIRROR_OAUTH_GITHUB_CLIENT_ID"`
	OAuthGitHubClientSecret    string   `env:"MIRROR_OAUTH_GITHUB_CLIENT_SECRET"`
	OAuthMicrosoftClientID     string   `env:"MIRROR_OAUTH_MICROSOFT_CLIENT_ID"`
	OAuthMicrosoftClientSecret string   `env:"MIRROR_OAUTH_MICROSOFT_CLIENT_SECRET"`
	InviteOnly                 bool     `env:"MIRROR_INVITE_ONLY"`
	DisableRegistration        bool     `env:"MIRROR_DISABLE_REGISTRATION"`
	EmailDomainWhitelist       []string `env:"MIRROR_EMAIL_DOMAIN_WHITELIST" envSeparator:","`

	StorageDriver string `env:"MIRROR_STORAGE_DRIVER" envDefault:"local"`
	S3Endpoint    string `env:"MIRROR_S3_ENDPOINT"`
	S3Region      string `env:"MIRROR_S3_REGION" envDefault:"auto"`
	S3Bucket      string `env:"MIRROR_S3_BUCKET"`
	S3AccessKey   string `env:"MIRROR_S3_ACCESS_KEY"`
	S3SecretKey   string `env:"MIRROR_S3_SECRET_KEY"`

	MailDriver   string `env:"MIRROR_MAIL_DRIVER" envDefault:"log"`
	SMTPAddr     string `env:"MIRROR_SMTP_ADDR"`
	SMTPFrom     string `env:"MIRROR_SMTP_FROM"`
	SMTPUser     string `env:"MIRROR_SMTP_USER"`
	SMTPPass     string `env:"MIRROR_SMTP_PASS"`
	ResendAPIKey string `env:"MIRROR_RESEND_API_KEY"`
	ResendFrom   string `env:"MIRROR_RESEND_FROM"`

	CDNDriver        string `env:"MIRROR_CDN_DRIVER"`
	CloudflareZoneID string `env:"MIRROR_CLOUDFLARE_ZONE_ID"`
	CloudflareAPIKey string `env:"MIRROR_CLOUDFLARE_API_KEY"`
	BunnyAPIKey      string `env:"MIRROR_BUNNY_API_KEY"`
	CDNWebhookURL    string `env:"MIRROR_CDN_WEBHOOK_URL"`

	RateLimitRPS   float64 `env:"MIRROR_RATE_LIMIT_RPS" envDefault:"100"`
	RateLimitBurst int     `env:"MIRROR_RATE_LIMIT_BURST" envDefault:"200"`
	AdminToken     string  `env:"MIRROR_ADMIN_TOKEN"`

	// 客户端 IP 解析：转发头顺序与可信代理网段（见 shared/clientip）。
	// 留空 = 默认头序（XFF→X-Real-IP→CF-Connecting-IP→True-Client-IP）+ 默认信任私网/环回代理。
	ClientIPHeaders   []string `env:"MIRROR_CLIENT_IP_HEADERS" envSeparator:","`
	TrustedProxies    []string `env:"MIRROR_TRUSTED_PROXIES" envSeparator:","`
	ClientIPTrustNone bool     `env:"MIRROR_CLIENT_IP_TRUST_NONE"`

	RedisAddr string `env:"MIRROR_REDIS_ADDR"`

	// 配额：默认存储上限（字节，0=不限）、按 storage_tier 覆盖（JSON 映射）、发布数量上限（0=不限）。
	QuotaStorageBytes int64  `env:"MIRROR_QUOTA_STORAGE_BYTES" envDefault:"67108864"`
	QuotaTiers        string `env:"MIRROR_QUOTA_TIERS"`
	QuotaPublished    int64  `env:"MIRROR_QUOTA_PUBLISHED" envDefault:"100"`

	// SkinReview 开启后新发布皮肤进入审核队列（under_review），管理员批准后才公开。
	SkinReview bool `env:"MIRROR_SKIN_REVIEW"`
}

// Load 从环境变量加载配置，缺省值见 .env.example。
func Load() *Config {
	cfg := &Config{}
	_ = env.Parse(cfg)
	return cfg
}
