package httpserver

import (
	"database/sql"
	"net/http"

	"github.com/redis/go-redis/v9"
)

func New(db *sql.DB, rdb *redis.Client, encryptionKey []byte) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	chatHandler := handleChatCompletions(db, rdb, encryptionKey)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, rdb, chatHandler))

	return mux
}
