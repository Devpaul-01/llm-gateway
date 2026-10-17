package credentials

import (
	"context"
	"database/sql"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/crypto"
)

func Insert(ctx context.Context, db *sql.DB, projectID, provider, label, plaintextAPIKey string, encryptionKey []byte) error {
	ciphertext, nonce, err := crypto.Encrypt([]byte(plaintextAPIKey), encryptionKey)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO provider_credentials (project_id, provider, label, encrypted_key, key_nonce, key_version)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, projectID, provider, label, ciphertext, nonce, 1)

	return err
}

func GetByProjectAndProvider(ctx context.Context, db *sql.DB, projectID, provider string, encryptionKey []byte) ([]Credential, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, provider, label, encrypted_key, key_nonce, status, created_at
		FROM provider_credentials
		WHERE project_id = $1 AND provider = $2 AND status = 'active'
	`, projectID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Credential
	for rows.Next() {
		var (
			c          Credential
			ciphertext []byte
			nonce      []byte
		)

		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Provider, &c.Label, &ciphertext, &nonce, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}

		plaintext, err := crypto.Decrypt(ciphertext, nonce, encryptionKey)
		if err != nil {
			return nil, err
		}
		c.APIKey = string(plaintext)

		results = append(results, c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// CredentialMetadata is a credential summary with no plaintext or ciphertext
// key material included, suitable for returning from an admin listing
// endpoint. Architecture Section 6.3: "Provider credentials are never
// returned by any API, admin or otherwise, once created -- only metadata
// (label, prefix if applicable, status, timestamps)."
type CredentialMetadata struct {
	ID        string
	ProjectID string
	Provider  string
	Label     string
	Status    string
	CreatedAt time.Time
}

// ListByProject returns metadata (no key material) for every credential
// belonging to a project, including inactive/revoked ones, ordered newest
// first. This is what backs the admin "list credentials" endpoint added to
// close the gap where ADR-04's "immediate revocation" had no way to be
// discovered or triggered via the API.
func ListByProject(ctx context.Context, db *sql.DB, projectID string) ([]CredentialMetadata, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, provider, label, status, created_at
		FROM provider_credentials
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []CredentialMetadata
	for rows.Next() {
		var m CredentialMetadata
		var label sql.NullString
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Provider, &label, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		if label.Valid {
			m.Label = label.String
		}
		results = append(results, m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// Revoke marks a credential as revoked (status = 'revoked') rather than
// deleting the row, preserving it for historical request_logs joins via
// credential_id. A revoked credential is excluded from
// GetByProjectAndProvider because that query filters on status = 'active'.
// Returns the number of rows affected so the caller can distinguish
// "not found" from "already revoked" / success.
func Revoke(ctx context.Context, db *sql.DB, projectID, credentialID string) (int64, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE provider_credentials
		SET status = 'revoked'
		WHERE id = $1 AND project_id = $2 AND status != 'revoked'
	`, credentialID, projectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
