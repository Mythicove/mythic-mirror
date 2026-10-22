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
	CaptchaDriver      string
	CaptchaSecret      string
	OAuthGitHubClientID    string
	OAuthGitHubClientSecret string
	InviteOnly         bool
	DisableRegistration bool
	EmailDomainWhitelist []string
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
		CaptchaDriver:      getenv("MIRROR_CAPTCHA_DRIVER", ""),
		CaptchaSecret:      getenv("MIRROR_CAPTCHA_SECRET", ""),
		OAuthGitHubClientID:    getenv("MIRROR_OAUTH_GITHUB_CLIENT_ID", ""),
		OAuthGitHubClientSecret: getenv("MIRROR_OAUTH_GITHUB_CLIENT_SECRET", ""),
		InviteOnly:         getenvBool("MIRROR_INVITE_ONLY"),
		DisableRegistration: getenvBool("MIRROR_DISABLE_REGISTRATION"),
		EmailDomainWhitelist: split(getenv("MIRROR_EMAIL_DOMAIN_WHITELIST", "")),
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
