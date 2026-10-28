package gatewaykeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

type GatewayKey struct {
	ID        string
	ProjectID string
	Label     string
	Status    string
}

// GatewayKeyMetadata is a gateway key summary with no hash/plaintext
// included, suitable for an admin listing endpoint.
type GatewayKeyMetadata struct {
	ID        string
	ProjectID string
	KeyPrefix string
	Label     string
	Status    string
	CreatedAt time.Time
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

// ListByProject returns metadata (hash never included) for every gateway
// key belonging to a project, newest first.
func ListByProject(ctx context.Context, db *sql.DB, projectID string) ([]GatewayKeyMetadata, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, project_id, key_prefix, label, status, created_at
		FROM gateway_api_keys
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []GatewayKeyMetadata
	for rows.Next() {
		var m GatewayKeyMetadata
		var label sql.NullString
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.KeyPrefix, &label, &m.Status, &m.CreatedAt); err != nil {
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

// Revoke marks a gateway key as revoked. Because LookupByPlaintext only
// matches status = 'active', a revoked key is rejected on the very next
// request -- this is what makes ADR-04's "immediate revocation" claim
// actually true via the API, rather than only true via direct DB access.
func Revoke(ctx context.Context, db *sql.DB, projectID, keyID string) (int64, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE gateway_api_keys
		SET status = 'revoked'
		WHERE id = $1 AND project_id = $2 AND status != 'revoked'
	`, keyID, projectID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
