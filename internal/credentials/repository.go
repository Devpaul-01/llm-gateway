package credentials

import (
	"context"
	"database/sql"

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
