// Package httpapi 组装 HTTP 路由与处理器。
package httpapi

import (
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/account"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
)

// NewRouter 组装 HTTP 路由。
func NewRouter(svc *yggdrasil.Service, acct *account.Service) http.Handler {
	mux := http.NewServeMux()
	svc.Register(mux)
	acct.RegisterRoutes(mux)
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
