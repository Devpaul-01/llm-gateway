package httpserver

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
)

type createProjectRequest struct {
	Name string `json:"name"`
}

type createProjectResponse struct {
	ID string `json:"id"`
}

type createGatewayKeyRequest struct {
	ProjectID string `json:"project_id"`
	Label     string `json:"label"`
}

type createGatewayKeyResponse struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	ProjectID string `json:"project_id"`
}

type createCredentialRequest struct {
	ProjectID string `json:"project_id"`
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	APIKey    string `json:"api_key"`
}

type projectResponse struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Status               string `json:"status"`
	RateLimitPerMin      *int   `json:"rate_limit_per_min"`
	MaxConcurrentStreams *int   `json:"max_concurrent_streams"`
	TokenBudget          *int   `json:"token_budget"`
	CreatedAt            string `json:"created_at"`
}

type updateProjectSettingsRequest struct {
	RateLimitPerMin      *int `json:"rate_limit_per_min"`
	MaxConcurrentStreams *int `json:"max_concurrent_streams"`
	TokenBudget          *int `json:"token_budget"`
}

func handleListProjects(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), `
			SELECT id, name, status, rate_limit_per_min, max_concurrent_streams, token_budget, created_at
			FROM projects
			ORDER BY created_at DESC
		`)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var results []projectResponse
		for rows.Next() {
			var p projectResponse
			var rateLimit, concurrency, budget sql.NullInt64
			var createdAt time.Time

			if err := rows.Scan(&p.ID, &p.Name, &p.Status, &rateLimit, &concurrency, &budget, &createdAt); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			if rateLimit.Valid {
				v := int(rateLimit.Int64)
				p.RateLimitPerMin = &v
			}
			if concurrency.Valid {
				v := int(concurrency.Int64)
				p.MaxConcurrentStreams = &v
			}
			if budget.Valid {
				v := int(budget.Int64)
				p.TokenBudget = &v
			}
			p.CreatedAt = createdAt.Format(time.RFC3339)

			results = append(results, p)
		}

		if err := rows.Err(); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

func handleGetProject(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")

		var p projectResponse
		var rateLimit, concurrency, budget sql.NullInt64
		var createdAt time.Time

		err := db.QueryRowContext(r.Context(), `
			SELECT id, name, status, rate_limit_per_min, max_concurrent_streams, token_budget, created_at
			FROM projects
			WHERE id = $1
		`, projectID).Scan(&p.ID, &p.Name, &p.Status, &rateLimit, &concurrency, &budget, &createdAt)

		if err == sql.ErrNoRows {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if rateLimit.Valid {
			v := int(rateLimit.Int64)
			p.RateLimitPerMin = &v
		}
		if concurrency.Valid {
			v := int(concurrency.Int64)
			p.MaxConcurrentStreams = &v
		}
		if budget.Valid {
			v := int(budget.Int64)
			p.TokenBudget = &v
		}
		p.CreatedAt = createdAt.Format(time.RFC3339)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	}
}

func handleUpdateProjectSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			http.Error(w, "project id is required", http.StatusBadRequest)
			return
		}

		var body updateProjectSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		_, err := db.ExecContext(r.Context(), `
			UPDATE projects
			SET rate_limit_per_min = $1, max_concurrent_streams = $2, token_budget = $3
			WHERE id = $4
		`, body.RateLimitPerMin, body.MaxConcurrentStreams, body.TokenBudget, projectID)
		if err != nil {
			http.Error(w, "failed to update project settings", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleCreateCredential(db *sql.DB, encryptionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.ProjectID == "" || body.Provider == "" || body.APIKey == "" {
			http.Error(w, "project_id, provider, and api_key are required", http.StatusBadRequest)
			return
		}

		err := credentials.Insert(r.Context(), db, body.ProjectID, body.Provider, body.Label, body.APIKey, encryptionKey)
		if err != nil {
			http.Error(w, "failed to create credential", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func handleCreateGatewayKey(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createGatewayKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.ProjectID == "" {
			http.Error(w, "project_id is required", http.StatusBadRequest)
			return
		}

		plaintext, keyID, err := gatewaykeys.Insert(r.Context(), db, body.ProjectID, body.Label)
		if err != nil {
			http.Error(w, "failed to create gateway key", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createGatewayKeyResponse{
			ID:        keyID,
			Key:       plaintext,
			ProjectID: body.ProjectID,
		})
	}
}

func handleCreateProject(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		var projectID string
		err := db.QueryRowContext(r.Context(), `
			INSERT INTO projects (name) VALUES ($1) RETURNING id
		`, body.Name).Scan(&projectID)
		if err != nil {
			http.Error(w, "failed to create project", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createProjectResponse{ID: projectID})
	}
}
