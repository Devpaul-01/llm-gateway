package gatewaykeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

type GatewayKey struct {
	ID        string
	ProjectID string
	Label     string
	Status    string
}

func GenerateKey() (plaintext string, err error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating key: %w", err)
	}
	return "gw_live_" + hex.EncodeToString(raw), nil
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func Insert(ctx context.Context, db *sql.DB, projectID, label string) (plaintext string, keyID string, err error) {
	plaintext, err = GenerateKey()
	if err != nil {
		return "", "", err
	}

	hash := hashKey(plaintext)
	prefix := plaintext[:12]

	err = db.QueryRowContext(ctx, `
		INSERT INTO gateway_api_keys (project_id, key_hash, key_prefix, label)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, projectID, hash, prefix, label).Scan(&keyID)
	if err != nil {
		return "", "", err
	}

	return plaintext, keyID, nil
}

func LookupByPlaintext(ctx context.Context, db *sql.DB, plaintext string) (*GatewayKey, error) {
	hash := hashKey(plaintext)

	var gk GatewayKey
	err := db.QueryRowContext(ctx, `
		SELECT id, project_id, label, status
		FROM gateway_api_keys
		WHERE key_hash = $1 AND status = 'active'
	`, hash).Scan(&gk.ID, &gk.ProjectID, &gk.Label, &gk.Status)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &gk, nil
}
