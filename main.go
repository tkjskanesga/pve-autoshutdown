package main

import (
	"log"
	"net/http"

	"automationshutdown/internal/config"
	"automationshutdown/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if len(cfg.PVEHosts) == 0 {
		log.Printf("warning: PVE_HOSTS is empty, Proxmox calls will fail until it is set")
	}
	if cfg.PVEToken == "" {
		log.Printf("warning: PVE_TOKEN is empty, Proxmox calls will fail until it is set")
	}
	srv := web.New(cfg)
	addr := ":" + cfg.Port
	log.Printf("automationshutdown listening on %s (frontend=%s dry-run=%v)", addr, cfg.FrontendMode, cfg.DryRun)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
}
