package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shinbunbun/peer-issuer/internal/config"
	"github.com/shinbunbun/peer-issuer/internal/db"
	"github.com/shinbunbun/peer-issuer/internal/handler"
	"github.com/shinbunbun/peer-issuer/internal/ippool"
	"github.com/shinbunbun/peer-issuer/internal/lease"
	"github.com/shinbunbun/peer-issuer/internal/routeros"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	pool, err := ippool.NewPool(cfg.CIPoolCIDR)
	if err != nil {
		log.Fatalf("create IP pool: %v", err)
	}

	router, err := routeros.NewClient(
		cfg.RouterHost,
		cfg.RouterSSHPort,
		cfg.RouterUser,
		cfg.RouterSSHKey,
		cfg.RouterHostKey,
		cfg.RouterWgIF,
	)
	if err != nil {
		log.Fatalf("create RouterOS client: %v", err)
	}

	svc := lease.NewService(
		database,
		pool,
		router,
		cfg.WGServerPubkey,
		cfg.WGEndpoint,
		cfg.WGMTU,
		cfg.WGKeepalive,
		cfg.DefaultTTL,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.StartCleaner(ctx)

	mux := handler.NewRouter(svc, router)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown: %v", err)
		}
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}

	log.Println("server stopped")
}
