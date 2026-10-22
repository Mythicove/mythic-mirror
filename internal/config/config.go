// Package config 从环境变量加载配置（M0 最小版，后续按需扩展）。
package config

import "os"

// Config 是服务运行所需的配置。
type Config struct {
	ListenAddr         string
	ServerName         string
	ImplementationName string
	SignaturePublicKey string
}

// Load 从环境变量读取配置，缺省值见 .env.example。
func Load() *Config {
	return &Config{
		ListenAddr:         getenv("MIRROR_LISTEN_ADDR", ":8080"),
		ServerName:         getenv("MIRROR_SERVER_NAME", "Mythic Mirror"),
		ImplementationName: getenv("MIRROR_IMPL_NAME", "mythic-mirror"),
		SignaturePublicKey: getenv("MIRROR_SIGNATURE_PUBLIC_KEY", ""),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
