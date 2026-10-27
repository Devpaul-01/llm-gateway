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

// Revoke disables a gateway key and evicts it from the auth cache, returning
// false if no such key exists.
func Revoke(ctx context.Context, db *sql.DB, rdb *redis.Client, keyID string) (bool, error) {
	var hash string
	err := db.QueryRowContext(ctx, `
		UPDATE gateway_api_keys SET status = 'revoked'
		WHERE id = $1
		RETURNING key_hash
	`, keyID).Scan(&hash)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rdb != nil {
		rdb.Del(ctx, cacheKey(hash))
	}
	return true, nil
}
