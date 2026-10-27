package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/applog"
	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/httpserver"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, relying on real environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	logger := applog.New()

	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database successfully")

	if cfg.AutoMigrate {
		if err := db.Migrate(ctx, pool, logger); err != nil {
			log.Fatalf("running migrations: %v", err)
		}
	}

	redisClient, err := redisclient.Connect(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("connecting to redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("connected to redis successfully")

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("decoding encryption key: %v", err)
	}

	defaults := projectsettings.Defaults{
		RateLimitPerMin:      cfg.DefaultRateLimitPerMin,
		MaxConcurrentStreams: cfg.DefaultMaxConcurrentStreams,
		TokenBudget:          cfg.DefaultTokenBudget,
	}

	mux := httpserver.New(pool, redisClient, encryptionKey, cfg.AdminToken, logger, defaults, httpserver.Options{
		CacheEnabled:           cfg.CacheEnabled,
		CacheTTL:               time.Duration(cfg.CacheTTLSeconds) * time.Second,
		PublicMetrics:          cfg.PublicMetrics,
		PublicMetricsProjectID: cfg.PublicMetricsProjectID,
		CORSOrigins:            cfg.CORSAllowedOrigins,
	})

	// No WriteTimeout on purpose: completions stream for as long as the model
	// talks. ReadHeaderTimeout/IdleTimeout still defend against slow-loris.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown: on SIGTERM (a deploy, a container stop) stop accepting
	// new requests but let in-flight streams finish instead of cutting them off.
	done := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down, draining in-flight requests...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown incomplete: %v", err)
		}
		close(done)
	}()

	log.Printf("starting server on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	<-done
}
