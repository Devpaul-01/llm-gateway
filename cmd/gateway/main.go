package main

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/httpserver"
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

	mux := httpserver.New(pool, redisClient, encryptionKey, cfg.AdminToken)
	log.Println("starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
