package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/Devpaul-01/llm-gateway/internal/config"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/db"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
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
	log.Println("config loaded")

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database")

	var projectID string
	err = pool.QueryRowContext(ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`, "dev-testing").Scan(&projectID)
	if err != nil {
		log.Fatalf("creating project: %v", err)
	}
	log.Printf("created project: %s", projectID)

	plaintext, keyID, err := gatewaykeys.Insert(ctx, pool, projectID, "dev-key")
	if err != nil {
		log.Fatalf("creating gateway key: %v", err)
	}
	log.Printf("created gateway key, id=%s, plaintext length=%d, plaintext prefix=%s", keyID, len(plaintext), plaintext[:12])

	groqAPIKey := os.Getenv("GROQ_API_KEY")
	if groqAPIKey == "" {
		log.Fatalf("GROQ_API_KEY not set in environment")
	}
	log.Println("found GROQ_API_KEY in environment")

	encryptionKey, err := hex.DecodeString(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("decoding encryption key: %v", err)
	}
	log.Println("decoded encryption key")

	if err := credentials.Insert(ctx, pool, projectID, "groq", "seed-groq-key", groqAPIKey, encryptionKey); err != nil {
		log.Fatalf("inserting groq credential: %v", err)
	}
	log.Println("inserted groq credential successfully")

	fmt.Println("=====================================")
	fmt.Println("Project ID:", projectID)
	fmt.Println("Gateway Key ID:", keyID)
	fmt.Println("Gateway Key (save this, shown once):", plaintext)
	fmt.Println("=====================================")
}