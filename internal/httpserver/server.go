package httpserver

import (
	"database/sql"
	"net/http"
)

func New(db *sql.DB, encryptionKey []byte) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	chatHandler := handleChatCompletions(db, encryptionKey)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, chatHandler))

	return mux
}
