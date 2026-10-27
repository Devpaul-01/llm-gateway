package httpserver

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/projectsettings"
	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/Devpaul-01/llm-gateway/internal/respcache"
	"github.com/redis/go-redis/v9"
)

// optionalInt distinguishes "field absent" (leave the setting alone) from
// "field null" (clear the override back to the default) from "field set".
type optionalInt struct {
	Set bool
	Val *int
}

func (o *optionalInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Val = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Val = &v
	return nil
}

func (o optionalInt) validate(name string) error {
	if o.Set && o.Val != nil && *o.Val <= 0 {
		return fmt.Errorf("%s must be a positive integer or null", name)
	}
	return nil
}

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
	RateLimitPerMin      optionalInt `json:"rate_limit_per_min"`
	MaxConcurrentStreams optionalInt `json:"max_concurrent_streams"`
	TokenBudget          optionalInt `json:"token_budget"`
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

func handleUpdateProjectSettings(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
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
		for name, f := range map[string]optionalInt{
			"rate_limit_per_min":     body.RateLimitPerMin,
			"max_concurrent_streams": body.MaxConcurrentStreams,
			"token_budget":           body.TokenBudget,
		} {
			if err := f.validate(name); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		// PATCH semantics: only fields present in the body change. A field set to
		// null clears the override (falls back to the server default).
		res, err := db.ExecContext(r.Context(), `
			UPDATE projects SET
			  rate_limit_per_min     = CASE WHEN $1::boolean THEN $2::integer ELSE rate_limit_per_min END,
			  max_concurrent_streams = CASE WHEN $3::boolean THEN $4::integer ELSE max_concurrent_streams END,
			  token_budget           = CASE WHEN $5::boolean THEN $6::integer ELSE token_budget END
			WHERE id = $7
		`, body.RateLimitPerMin.Set, body.RateLimitPerMin.Val,
			body.MaxConcurrentStreams.Set, body.MaxConcurrentStreams.Val,
			body.TokenBudget.Set, body.TokenBudget.Val, projectID)
		if err != nil {
			http.Error(w, "failed to update project settings", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}

		// Settings are cached; evict so the change applies on the very next request.
		projectsettings.Invalidate(r.Context(), rdb, projectID)

		w.WriteHeader(http.StatusOK)
	}
}

func handleCreateCredential(db *sql.DB, rdb *redis.Client, encryptionKey []byte) http.HandlerFunc {
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

		if !providers.IsKnownProvider(body.Provider) {
			http.Error(w, fmt.Sprintf("unknown provider %q, supported: %s", body.Provider, strings.Join(providers.KnownProviders(), ", ")), http.StatusBadRequest)
			return
		}

		err := credentials.Insert(r.Context(), db, body.ProjectID, body.Provider, body.Label, body.APIKey, encryptionKey)
		if err != nil {
			http.Error(w, "failed to create credential", http.StatusInternalServerError)
			return
		}
		credentials.Invalidate(r.Context(), rdb, body.ProjectID, body.Provider)

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

func handleRevokeGatewayKey(db *sql.DB, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		found, err := gatewaykeys.Revoke(r.Context(), db, rdb, r.PathValue("id"))
		if err != nil {
			http.Error(w, "failed to revoke gateway key", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "gateway key not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleFlushCache invalidates every cached response of a project at once.
func handleFlushCache(rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := respcache.Invalidate(r.Context(), rdb, r.PathValue("id")); err != nil {
			http.Error(w, "failed to flush cache", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"providers": providers.KnownProviders()})
	}
}
