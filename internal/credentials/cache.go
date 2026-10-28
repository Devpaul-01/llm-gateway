package credentials

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/crypto"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute

// cachedCred is what lands in Redis. Only ciphertext is cached, never the
// plaintext provider key, so a Redis compromise does not leak upstream keys.
// Decrypting AES-GCM in-process costs microseconds.
type cachedCred struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Ciphertext []byte    `json:"ct"`
	Nonce      []byte    `json:"n"`
	CreatedAt  time.Time `json:"c"`
}

func cacheKey(projectID, provider string) string {
	return fmt.Sprintf("creds:%s:%s", projectID, provider)
}

// Invalidate drops the cached credential list for a project/provider. Call it
// whenever credentials are added, rotated or disabled.
func Invalidate(ctx context.Context, rdb *redis.Client, projectID, provider string) {
	if rdb == nil {
		return
	}
	rdb.Del(ctx, cacheKey(projectID, provider))
}

// GetManyCached returns the active credentials of a project for several
// providers at once: one Redis MGET for the hits and at most one SQL query for
// all the misses, instead of one query per provider on every request.
func GetManyCached(ctx context.Context, db *sql.DB, rdb *redis.Client, projectID string, providers []string, encryptionKey []byte) (map[string][]Credential, error) {
	rows := make(map[string][]cachedCred, len(providers))
	var missing []string

	if rdb != nil && len(providers) > 0 {
		keys := make([]string, len(providers))
		for i, p := range providers {
			keys[i] = cacheKey(projectID, p)
		}
		vals, err := rdb.MGet(ctx, keys...).Result()
		if err == nil {
			for i, v := range vals {
				s, ok := v.(string)
				if !ok {
					missing = append(missing, providers[i])
					continue
				}
				var list []cachedCred
				if json.Unmarshal([]byte(s), &list) != nil {
					missing = append(missing, providers[i])
					continue
				}
				rows[providers[i]] = list
			}
		} else {
			missing = providers
		}
	} else {
		missing = providers
	}

	if len(missing) > 0 {
		fetched, err := queryCredentialRows(ctx, db, projectID, missing)
		if err != nil {
			return nil, err
		}
		for _, p := range missing {
			list := fetched[p]
			rows[p] = list
			if rdb != nil {
				if data, err := json.Marshal(list); err == nil {
					// An empty list is cached too, so a project with no key for a
					// provider doesn't hit the database on every request.
					rdb.Set(ctx, cacheKey(projectID, p), data, cacheTTL)
				}
			}
		}
	}

	out := make(map[string][]Credential, len(providers))
	for p, list := range rows {
		for _, c := range list {
			plaintext, err := crypto.Decrypt(c.Ciphertext, c.Nonce, encryptionKey)
			if err != nil {
				return nil, err
			}
			out[p] = append(out[p], Credential{
				ID:        c.ID,
				ProjectID: projectID,
				Provider:  p,
				Label:     c.Label,
				APIKey:    string(plaintext),
				Status:    "active",
				CreatedAt: c.CreatedAt,
			})
		}
	}
	return out, nil
}

func queryCredentialRows(ctx context.Context, db *sql.DB, projectID string, providers []string) (map[string][]cachedCred, error) {
	r, err := db.QueryContext(ctx, `
		SELECT id, provider, COALESCE(label, ''), encrypted_key, key_nonce, created_at
		FROM provider_credentials
		WHERE project_id = $1 AND provider = ANY($2) AND status = 'active'
		ORDER BY created_at, id
	`, projectID, providers)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	out := make(map[string][]cachedCred)
	for r.Next() {
		var c cachedCred
		var provider string
		if err := r.Scan(&c.ID, &provider, &c.Label, &c.Ciphertext, &c.Nonce, &c.CreatedAt); err != nil {
			return nil, err
		}
		out[provider] = append(out[provider], c)
	}
	return out, r.Err()
}

// RevokeAndInvalidate revokes a credential and evicts the cached credential
// list of its provider, so the revoked key stops being used on the very next
// request instead of lingering until the cache TTL. Returns rows affected.
func RevokeAndInvalidate(ctx context.Context, db *sql.DB, rdb *redis.Client, projectID, credentialID string) (int64, error) {
	var provider string
	err := db.QueryRowContext(ctx, `
		UPDATE provider_credentials
		SET status = 'revoked'
		WHERE id = $1 AND project_id = $2 AND status != 'revoked'
		RETURNING provider
	`, credentialID, projectID).Scan(&provider)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	Invalidate(ctx, rdb, projectID, provider)
	return 1, nil
}
