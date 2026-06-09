// Command recipearr watches recipe RSS feeds, filters by keyword, and imports matching
// recipes into a Tandoor server with an import-quality enrichment layer.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"recipearr/internal/config"
	"recipearr/internal/pipeline"
	"recipearr/internal/scheduler"
	"recipearr/internal/store"
	"recipearr/internal/web"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("recipearr: data dir %s: %v", cfg.DataDir, err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("recipearr: open db: %v", err)
	}
	defer st.Close()

	// Seed Tandoor URL/token from the environment on first run only (never overwrite).
	seedSetting(st, "tandoor_url", cfg.SeedTandoorURL)
	seedSetting(st, "tandoor_token", cfg.SeedTandoorToken)

	svc := pipeline.New(st)
	sch := scheduler.New(st, svc)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sch.Run(ctx)

	srv := &http.Server{
		Addr:    cfg.BindAddr,
		Handler: web.New(st, svc).Handler(),
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Printf("recipearr listening on http://%s  (data: %s)", cfg.BindAddr, cfg.DataDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("recipearr: server: %v", err)
	}
	log.Println("recipearr: shut down cleanly")
}

func seedSetting(st *store.Store, key, val string) {
	if val == "" {
		return
	}
	if existing, _ := st.GetSetting(key, ""); existing == "" {
		_ = st.SetSetting(key, val)
	}
}
