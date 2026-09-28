package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const cacheTTL = 6 * time.Hour
const lockTTL = 15 * time.Second

type modelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func EvictModel(ctx context.Context, rdb *redis.Client, provider, model string) {
	cacheKey := fmt.Sprintf("discovery:%s", provider)

	cached, err := rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		return
	}

	var models []string
	if err := json.Unmarshal([]byte(cached), &models); err != nil {
		return
	}

	updated := make([]string, 0, len(models))
	for _, m := range models {
		if m != model {
			updated = append(updated, m)
		}
	}
	if len(updated) == len(models) {
		return
	}

	data, err := json.Marshal(updated)
	if err != nil {
		return
	}

	ttl, err := rdb.TTL(ctx, cacheKey).Result()
	if err != nil || ttl <= 0 {
		ttl = cacheTTL
	}
	rdb.Set(ctx, cacheKey, data, ttl)
}

func RecordProviderTransient(ctx context.Context, rdb *redis.Client, provider string) {
	key := fmt.Sprintf("providerhealth:%s", provider)
	pipe := rdb.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, time.Hour)
	pipe.Exec(ctx)
}

func fetchModelsFromProvider(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery request failed: HTTP %d", resp.StatusCode)
	}

	var parsed modelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	models := make([]string, len(parsed.Data))
	for i, m := range parsed.Data {
		models[i] = m.ID
	}
	return models, nil
}

func GetModels(ctx context.Context, rdb *redis.Client, provider, baseURL, apiKey string, fallback []string) []string {
	cacheKey := fmt.Sprintf("discovery:%s", provider)

	cached, err := rdb.Get(ctx, cacheKey).Result()
	if err == nil {
		var models []string
		if jsonErr := json.Unmarshal([]byte(cached), &models); jsonErr == nil {
			return models
		}
	}

	lockKey := fmt.Sprintf("discovery:%s:lock", provider)
	acquired, err := rdb.SetNX(ctx, lockKey, "1", lockTTL).Result()
	if err != nil || !acquired {
		return fallback
	}
	defer rdb.Del(context.Background(), lockKey)

	models, err := fetchModelsFromProvider(ctx, baseURL, apiKey)
	if err != nil {
		return fallback
	}

	data, err := json.Marshal(models)
	if err == nil {
		rdb.Set(ctx, cacheKey, data, cacheTTL)
	}

	return models
}
