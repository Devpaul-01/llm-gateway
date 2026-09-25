package httpserver

import (
	"database/sql"
	"log/slog"

	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"

	"net/http"

	"github.com/redis/go-redis/v9"
)

func New(db *sql.DB, rdb *redis.Client, encryptionKey []byte, adminToken string, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	chatHandler := handleChatCompletions(db, rdb, encryptionKey, logger)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, rdb, chatHandler))

	mux.Handle("POST /admin/projects", RequireAdminToken(adminToken, handleCreateProject(db)))
	mux.Handle("POST /admin/gateway-keys", RequireAdminToken(adminToken, handleCreateGatewayKey(db)))
	mux.Handle("POST /admin/credentials", RequireAdminToken(adminToken, handleCreateCredential(db, encryptionKey)))
	mux.Handle("PATCH /admin/projects/{id}", RequireAdminToken(adminToken, projectsettings.HandleUpdateProjectSettings(db)))
	return mux
}
