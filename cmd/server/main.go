package main

import (
	"log"
	"net/http"

	"github.com/Mythicove/mythic-mirror/internal/config"
	"github.com/Mythicove/mythic-mirror/internal/filestorage"
	"github.com/Mythicove/mythic-mirror/internal/httpapi"
	"github.com/Mythicove/mythic-mirror/internal/keys"
	"github.com/Mythicove/mythic-mirror/internal/yggdrasil"
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

	svc := yggdrasil.NewService(yggdrasil.NewMemoryStore(), kp, files, yggdrasil.ServiceOptions{
		ServerName:         cfg.ServerName,
		ImplementationName: cfg.ImplementationName,
		SkinDomains:        cfg.SkinDomains,
		TexturesBaseURL:    cfg.TexturesBaseURL,
	})

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: httpapi.NewRouter(svc),
	}

	log.Printf("mythic-mirror listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
