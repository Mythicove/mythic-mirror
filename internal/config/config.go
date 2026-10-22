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
	SkinDomains        []string
	TexturesBaseURL    string
}

// Load 从环境变量读取配置，缺省值见 .env.example。
func Load() *Config {
	return &Config{
		ListenAddr:         getenv("MIRROR_LISTEN_ADDR", ":8080"),
		ServerName:         getenv("MIRROR_SERVER_NAME", "Mythic Mirror"),
		ImplementationName: getenv("MIRROR_IMPL_NAME", "mythic-mirror"),
		KeyPath:            getenv("MIRROR_KEY_PATH", "data/private.pem"),
		SkinDomains:        split(getenv("MIRROR_SKIN_DOMAINS", "localhost")),
		TexturesBaseURL:    getenv("MIRROR_TEXTURES_BASE_URL", "http://localhost:8080"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
