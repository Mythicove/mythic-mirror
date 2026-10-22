// Package httpapi 组装 HTTP 路由与处理器。
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/config"
)

// NewRouter 组装 HTTP 路由。
func NewRouter(cfg *config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		handleMeta(cfg, w, r)
	})
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func handleMeta(cfg *config.Config, w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"meta": map[string]any{
			"serverName":            cfg.ServerName,
			"implementationName":    cfg.ImplementationName,
			"implementationVersion": "0.0.1",
		},
		"skinDomains":        []string{"localhost"},
		"signaturePublickey": cfg.SignaturePublicKey,
	})
}
