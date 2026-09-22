package httpserver

import (
	"database/sql"
	"github.com/Devpaul-01/llm-gateway/internal/gatewaykeys"
	"encoding/json"
	"github.com/Devpaul-01/llm-gateway/internal/credentials"
	"net/http"
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