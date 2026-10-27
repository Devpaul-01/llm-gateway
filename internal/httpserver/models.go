package httpserver

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/redis/go-redis/v9"
)

// handleListModels implements GET /v1/models, which most OpenAI-compatible
// clients and UIs call on startup to populate a model picker. It lists the
// providers this project has credentials for, plus the models the gateway can
// route by bare name for them.
func handleListModels(db *sql.DB, rdb *redis.Client, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := ProjectIDFromContext(r.Context())
		if !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}

		rows, err := db.QueryContext(r.Context(), `
			SELECT DISTINCT provider FROM provider_credentials
			WHERE project_id = $1 AND status = 'active'
		`, projectID)
		if err != nil {
			logger.Error("listing providers failed", "error", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		defer rows.Close()

		have := map[string]bool{}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
				return
			}
			have[p] = true
		}

		type modelObj struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}
		var data []modelObj
		for model, provider := range providers.ModelCatalog() {
			if have[provider] {
				data = append(data, modelObj{ID: model, Object: "model", Created: time.Now().Unix(), OwnedBy: provider})
			}
		}
		sort.Slice(data, func(i, j int) bool { return data[i].ID < data[j].ID })
		if data == nil {
			data = []modelObj{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data})
	}
}
