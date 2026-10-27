// Package respcache is an exact-match response cache for deterministic
// requests (temperature explicitly 0), backed by Redis.
//
// Invalidation is epoch-based: every project has a counter, and cache keys
// embed its current value. Bumping the counter (Invalidate) makes every
// previous entry for that project unreachable in O(1), with no SCAN/DEL
// storm. The orphaned entries simply age out via their TTL.
package respcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/providers"
	"github.com/redis/go-redis/v9"
)

const maxCacheablePromptBytes = 256 * 1024

type Entry struct {
	Content      string          `json:"content"`
	FinishReason string          `json:"finish_reason"`
	Usage        providers.Usage `json:"usage"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	CreatedAt    int64           `json:"created_at"`
}

func epochKey(projectID string) string { return "respcache:epoch:" + projectID }
func prefix(projectID string) string   { return "respcache:" + projectID }

// Lookup returns the entry and the epoch it was looked up under, atomically,
// in a single round trip.
var getScript = redis.NewScript(`
local e = redis.call('GET', KEYS[1])
if not e then e = '0' end
local v = redis.call('GET', ARGV[1] .. ':' .. e .. ':' .. ARGV[2])
return {e, v or false}
`)

// Store writes only if the epoch hasn't moved since the lookup, so a response
// that was in flight during an Invalidate can't repopulate the cache with a
// pre-invalidation answer.
var setScript = redis.NewScript(`
local e = redis.call('GET', KEYS[1])
if not e then e = '0' end
if e ~= ARGV[3] then return 0 end
redis.call('SET', ARGV[1] .. ':' .. e .. ':' .. ARGV[2], ARGV[4], 'EX', ARGV[5])
return 1
`)

// Cacheable reports whether a request's answer may be cached or replayed.
// Only requests that explicitly ask for temperature 0 qualify: anything else is
// sampled, and replaying one sample as "the" answer would change behaviour.
func Cacheable(req providers.Request) bool {
	if !req.TemperatureSet || req.Temperature != 0 {
		return false
	}
	size := 0
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			return false
		}
		size += len(m.Content)
	}
	return size > 0 && size <= maxCacheablePromptBytes
}

// Key is a stable digest of everything that can change the answer.
func Key(req providers.Request) string {
	type msg struct {
		R string `json:"r"`
		C string `json:"c"`
	}
	canon := struct {
		V         int      `json:"v"`
		Provider  string   `json:"p"`
		Model     string   `json:"m"`
		Fallbacks []string `json:"f"`
		MaxTokens int      `json:"t"`
		Messages  []msg    `json:"msgs"`
	}{V: 1, Provider: req.Provider, Model: req.Model, Fallbacks: req.FallbackModels, MaxTokens: req.MaxTokens}
	for _, m := range req.Messages {
		canon.Messages = append(canon.Messages, msg{R: m.Role, C: m.Content})
	}
	data, _ := json.Marshal(canon)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Get returns the cached entry (nil on a miss) plus the epoch to pass to Set.
func Get(ctx context.Context, rdb *redis.Client, projectID, key string) (*Entry, string, error) {
	res, err := getScript.Run(ctx, rdb, []string{epochKey(projectID)}, prefix(projectID), key).Slice()
	if err != nil {
		return nil, "", err
	}
	if len(res) != 2 {
		return nil, "", errors.New("unexpected cache lookup result")
	}
	epoch, _ := res[0].(string)
	raw, ok := res[1].(string)
	if !ok {
		return nil, epoch, nil
	}
	var e Entry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil, epoch, nil
	}
	return &e, epoch, nil
}

func Set(ctx context.Context, rdb *redis.Client, projectID, key, epoch string, e Entry, ttl time.Duration) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return setScript.Run(ctx, rdb, []string{epochKey(projectID)}, prefix(projectID), key, epoch, string(data), int(ttl.Seconds())).Err()
}

// Invalidate makes every cached response of the project unreachable immediately.
func Invalidate(ctx context.Context, rdb *redis.Client, projectID string) error {
	return rdb.Incr(ctx, epochKey(projectID)).Err()
}

// Replay turns a cached entry back into the chunk stream the handlers expect.
func Replay(e *Entry, requestID string, started time.Time) <-chan providers.Chunk {
	ch := make(chan providers.Chunk, 2)
	ch <- providers.Chunk{Content: e.Content, Provider: e.Provider, Model: e.Model}
	ttft := int(time.Since(started).Milliseconds())
	ch <- providers.Chunk{
		Done:         true,
		Usage:        &e.Usage,
		FinishReason: e.FinishReason,
		Provider:     e.Provider,
		Model:        e.Model,
		Summary: &providers.Summary{
			RequestID:    requestID,
			Provider:     e.Provider,
			Model:        e.Model,
			FinishReason: e.FinishReason,
			Usage:        e.Usage,
			TTFTMs:       &ttft,
			TotalMs:      ttft,
			Attempts:     0,
			CacheHit:     true,
			ModelsUsed:   []providers.ModelUsage{{Provider: e.Provider, Model: e.Model}},
		},
	}
	close(ch)
	return ch
}

// Tap forwards a chunk stream unchanged and, if it ends in a clean first-try
// completion, stores the answer. Failed-over, continued or truncated answers
// are never cached.
func Tap(ctx context.Context, rdb *redis.Client, logger *slog.Logger, projectID, key, epoch string, ttl time.Duration, in <-chan providers.Chunk) <-chan providers.Chunk {
	out := make(chan providers.Chunk)
	go func() {
		defer close(out)
		var content []byte
		for c := range in {
			content = append(content, c.Content...)
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
			if c.Done && c.Summary != nil {
				s := c.Summary
				if s.Attempts == 1 && s.FinishReason == "stop" && len(content) > 0 && len(s.ModelsUsed) == 1 {
					sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					err := Set(sctx, rdb, projectID, key, epoch, Entry{
						Content:      string(content),
						FinishReason: s.FinishReason,
						Usage:        s.Usage,
						Provider:     s.Provider,
						Model:        s.Model,
						CreatedAt:    time.Now().Unix(),
					}, ttl)
					cancel()
					if err != nil {
						logger.Error("failed to store cached response", "error", err)
					}
				}
			}
		}
	}()
	return out
}
