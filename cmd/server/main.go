package main

import (
	"log"
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/config"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
)

func main() {
	cfg := config.Load()

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpapi.NewRouter(cfg),
	}

	log.Printf("mythic-mirror listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
