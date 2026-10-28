package gatewaykeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	positiveCacheTTL = 5 * time.Minute
	// Unknown keys are cached briefly too, so a client spamming bad keys can't
	// turn every attempt into a database query.
	negativeCacheTTL = 15 * time.Second
	negativeMarker   = "-"
)

func cacheKey(hash string) string { return "gwkey:" + hash }

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

// LookupCached is LookupByPlaintext with a Redis read-through cache in front.
// Auth runs on every request, so this removes a database round trip from the
// hot path. Revoke and Insert invalidate the entry, so revocation is immediate.
func LookupCached(ctx context.Context, db *sql.DB, rdb *redis.Client, plaintext string) (*GatewayKey, error) {
	if rdb == nil {
		return LookupByPlaintext(ctx, db, plaintext)
	}
	hash := hashKey(plaintext)
	key := cacheKey(hash)

	if cached, err := rdb.Get(ctx, key).Result(); err == nil {
		if cached == negativeMarker {
			return nil, nil
		}
		var gk GatewayKey
		if json.Unmarshal([]byte(cached), &gk) == nil {
			return &gk, nil
		}
	}

	gk, err := LookupByPlaintext(ctx, db, plaintext)
	if err != nil {
		return nil, err
	}
	if gk == nil {
		rdb.Set(ctx, key, negativeMarker, negativeCacheTTL)
		return nil, nil
	}
	if data, err := json.Marshal(gk); err == nil {
		rdb.Set(ctx, key, data, positiveCacheTTL)
	}
	return gk, nil
}

// InvalidatePlaintext drops any cached entry (including a negative one) for a key.
func InvalidatePlaintext(ctx context.Context, rdb *redis.Client, plaintext string) {
	if rdb == nil {
		return
	}
	rdb.Del(ctx, cacheKey(hashKey(plaintext)))
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
	return results, rows.Err()
}

// Revoke marks a gateway key as revoked and returns rows affected. Because
// LookupByPlaintext only matches status = 'active', the database is
// immediately authoritative. Callers that run with the auth cache enabled
// should use RevokeAndInvalidate so the cached entry is evicted too.
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

// RevokeAndInvalidate revokes a key and evicts it from the Redis auth cache,
// so revocation takes effect on the very next request rather than after the
// cache TTL. Returns rows affected.
func RevokeAndInvalidate(ctx context.Context, db *sql.DB, rdb *redis.Client, projectID, keyID string) (int64, error) {
	var hash string
	err := db.QueryRowContext(ctx, `
		UPDATE gateway_api_keys SET status = 'revoked'
		WHERE id = $1 AND project_id = $2 AND status != 'revoked'
		RETURNING key_hash
	`, keyID, projectID).Scan(&hash)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if rdb != nil {
		rdb.Del(ctx, cacheKey(hash))
	}
	return 1, nil
}
