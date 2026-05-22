package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cpuguard/internal/api"
	"cpuguard/internal/config"
	"cpuguard/internal/engine"
	"cpuguard/internal/store"
	"cpuguard/internal/system"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "/etc/cpuguard/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := system.Preflight(); err != nil {
		log.Fatalf("preflight: %v", err)
	}
	db, err := store.Open(cfg.Global.StateDB)
	if err != nil {
		log.Fatalf("open state db: %v", err)
	}
	defer db.Close()

	eng := engine.New(cfg, db)
	server := api.NewServer(eng, configPath, cfg.Global.SocketPath)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go eng.Run(ctx)
	go func() {
		if err := server.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("api server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("api shutdown: %v", err)
	}
}
