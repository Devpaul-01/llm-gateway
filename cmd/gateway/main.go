package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/applog"
	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/httpserver"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/redisclient"
	"github.com/joho/godotenv"
)

// runHealthcheckMode is used as the container's HEALTHCHECK command
// (docker-compose.yml). The distroless base image this binary ships in
// has no shell, no curl, and no wget -- the usual CMD-SHELL "curl
// localhost:8080/healthz" pattern is unavailable -- so the binary checks
// its own health endpoint and exits 0/1 accordingly, giving Docker (and,
// downstream, a PaaS that respects container health status) a real
// signal without needing any additional tooling in the final image.
func runHealthcheckMode() {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:8080/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck request failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck returned status", resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		runHealthcheckMode()
		return
	}

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

	mux := httpserver.New(pool, redisClient, encryptionKey, cfg.AdminToken, logger, defaults)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	httpserver.RunWithGracefulShutdown(srv, logger)
}