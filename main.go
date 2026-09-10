// Command cairn runs the task-and-notebook web app.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	_ "time/tzdata" // embed the zoneinfo DB so TZ / ICS TZID work in a scratch container

	"taskmanager/internal/store"
	"taskmanager/internal/web"
)

func main() {
	defaultAddr := envOr("CAIRN_ADDR", ":8082")
	if p := os.Getenv("PORT"); p != "" {
		defaultAddr = ":" + p
	}
	addr := flag.String("addr", defaultAddr, "listen address")
	dbPath := flag.String("db", envOr("CAIRN_DB", filepath.Join("data", "cairn.db")), "SQLite database path")
	siteName := flag.String("site-name", envOr("CAIRN_SITE_NAME", "Cairn"), "site name shown in the UI")
	demo := flag.Bool("demo", envBool("CAIRN_DEMO", true), "create a demo account with sample data when the database is empty")
	secret := flag.String("secret", os.Getenv("CAIRN_SECRET"), "server secret for CSRF/signing (generated and stored if empty)")
	secureCookies := flag.Bool("secure-cookies", os.Getenv("CAIRN_SECURE_COOKIES") == "1",
		"always mark cookies Secure (use behind HTTPS/TLS-terminating proxy)")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if *demo {
		created, err := store.SeedDemo(db)
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		if created {
			log.Printf("demo account ready — email: demo@cairn.local  password: demodemo")
		}
	}

	serverSecret, err := db.ServerSecret(*secret)
	if err != nil {
		log.Fatalf("server secret: %v", err)
	}

	srv, err := web.New(db, web.Config{
		SiteName:      *siteName,
		Secret:        serverSecret,
		SecureCookies: *secureCookies,
	})
	if err != nil {
		log.Fatalf("build server: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           web.RequestLog(srv),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("%s listening on http://localhost%s", *siteName, *addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
