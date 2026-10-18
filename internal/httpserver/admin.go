package httpserver

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"github.com/Devpaul-01/llm-gateway/internal/requestlog"
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

// credentialMetadataResponse never includes key material, per
// Architecture Section 6.3: "Provider credentials are never returned by
// any API, admin or otherwise, once created -- only metadata."
type credentialMetadataResponse struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	Provider   string  `json:"provider"`
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

type gatewayKeyMetadataResponse struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"project_id"`
	KeyPrefix  string  `json:"key_prefix"`
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func handleListProjects(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), `
			SELECT id, name, status, rate_limit_per_min, max_concurrent_streams, token_budget, created_at
			FROM projects
			ORDER BY created_at DESC
		`)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		defer rows.Close()

		var results []projectResponse
		for rows.Next() {
			var p projectResponse
			var rateLimit, concurrency, budget sql.NullInt64
			var createdAt time.Time

			if err := rows.Scan(&p.ID, &p.Name, &p.Status, &rateLimit, &concurrency, &budget, &createdAt); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
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
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
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
			writeJSONError(w, http.StatusNotFound, "not_found_error", "project not found")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal error")
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
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project id is required")
			return
		}

		var body updateProjectSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
			return
		}

		_, err := db.ExecContext(r.Context(), `
			UPDATE projects
			SET rate_limit_per_min = $1, max_concurrent_streams = $2, token_budget = $3
			WHERE id = $4
		`, body.RateLimitPerMin, body.MaxConcurrentStreams, body.TokenBudget, projectID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to update project settings")
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleCreateCredential(db *sql.DB, encryptionKey []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
			return
		}
		if body.ProjectID == "" || body.Provider == "" || body.APIKey == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project_id, provider, and api_key are required")
			return
		}

		err := credentials.Insert(r.Context(), db, body.ProjectID, body.Provider, body.Label, body.APIKey, encryptionKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to create credential")
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// handleListCredentials backs GET /admin/projects/{id}/credentials.
// Closes the gap identified in Audit §7.2: there was previously no way to
// discover what credentials exist for a project via the API at all. Never
// returns key material -- only metadata, consistent with Section 6.3.
// Includes a best-effort last_used_at derived from request_logs per
// ADR-18, now meaningful because credential_id is actually populated on
// insert (Finding B-6's fix).
func handleListCredentials(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project id is required")
			return
		}

		meta, err := credentials.ListByProject(r.Context(), db, projectID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to list credentials")
			return
		}

		results := make([]credentialMetadataResponse, 0, len(meta))
		for _, m := range meta {
			resp := credentialMetadataResponse{
				ID:        m.ID,
				ProjectID: m.ProjectID,
				Provider:  m.Provider,
				Label:     m.Label,
				Status:    m.Status,
				CreatedAt: m.CreatedAt.Format(time.RFC3339),
			}
			if lastUsed, err := requestlog.LastUsedAt(r.Context(), db, m.ID); err == nil && lastUsed.Valid {
				s := lastUsed.Time.Format(time.RFC3339)
				resp.LastUsedAt = &s
			}
			results = append(results, resp)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// handleRevokeCredential backs DELETE /admin/projects/{id}/credentials/{credentialID}.
// This is the revocation half of Audit §7.2 / ADR-04's "immediate
// revocation" justification for not storing a reversible secret: a
// revoked credential is excluded from GetByProjectAndProvider on the very
// next candidate resolution.
func handleRevokeCredential(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		credentialID := r.PathValue("credentialID")
		if projectID == "" || credentialID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project id and credential id are required")
			return
		}

		affected, err := credentials.Revoke(r.Context(), db, projectID, credentialID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to revoke credential")
			return
		}
		if affected == 0 {
			writeJSONError(w, http.StatusNotFound, "not_found_error", "credential not found or already revoked")
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleCreateGatewayKey(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createGatewayKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
			return
		}
		if body.ProjectID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project_id is required")
			return
		}

		plaintext, keyID, err := gatewaykeys.Insert(r.Context(), db, body.ProjectID, body.Label)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to create gateway key")
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

// handleListGatewayKeys backs GET /admin/projects/{id}/gateway-keys.
// Same justification as handleListCredentials -- never returns the hash
// or any reconstructable secret, only the display prefix and metadata.
func handleListGatewayKeys(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		if projectID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project id is required")
			return
		}

		meta, err := gatewaykeys.ListByProject(r.Context(), db, projectID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to list gateway keys")
			return
		}

		results := make([]gatewayKeyMetadataResponse, 0, len(meta))
		for _, m := range meta {
			results = append(results, gatewayKeyMetadataResponse{
				ID:        m.ID,
				ProjectID: m.ProjectID,
				KeyPrefix: m.KeyPrefix,
				Label:     m.Label,
				Status:    m.Status,
				CreatedAt: m.CreatedAt.Format(time.RFC3339),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}

// handleRevokeGatewayKey backs DELETE /admin/projects/{id}/gateway-keys/{keyID}.
// After this call, LookupByPlaintext stops matching the key (it filters
// on status = 'active'), so RequireGatewayKey rejects the very next
// request made with it.
func handleRevokeGatewayKey(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("id")
		keyID := r.PathValue("keyID")
		if projectID == "" || keyID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "project id and key id are required")
			return
		}

		affected, err := gatewaykeys.Revoke(r.Context(), db, projectID, keyID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to revoke gateway key")
			return
		}
		if affected == 0 {
			writeJSONError(w, http.StatusNotFound, "not_found_error", "gateway key not found or already revoked")
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleCreateProject(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body createProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
			return
		}
		if body.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "name is required")
			return
		}

		var projectID string
		err := db.QueryRowContext(r.Context(), `
			INSERT INTO projects (name) VALUES ($1) RETURNING id
		`, body.Name).Scan(&projectID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to create project")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createProjectResponse{ID: projectID})
	}
}
