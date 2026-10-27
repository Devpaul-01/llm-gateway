package httpserver

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/redis/go-redis/v9"
)

func New(db *sql.DB, rdb *redis.Client, encryptionKey []byte, adminToken string, logger *slog.Logger, defaults projectsettings.Defaults) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	chatHandler := handleChatCompletions(db, rdb, encryptionKey, logger, defaults)
	mux.Handle("POST /v1/chat/completions", RequireGatewayKey(db, rdb, logger, defaults, chatHandler))

	mux.Handle("GET /admin/projects", RequireAdminToken(adminToken, handleListProjects(db)))
	mux.Handle("GET /admin/projects/{id}", RequireAdminToken(adminToken, handleGetProject(db)))
	mux.Handle("POST /admin/projects", RequireAdminToken(adminToken, handleCreateProject(db)))
	mux.Handle("POST /admin/gateway-keys", RequireAdminToken(adminToken, handleCreateGatewayKey(db)))
	mux.Handle("POST /admin/credentials", RequireAdminToken(adminToken, handleCreateCredential(db, encryptionKey)))
	mux.Handle("PATCH /admin/projects/{id}", RequireAdminToken(adminToken, handleUpdateProjectSettings(db)))

	return mux
}
