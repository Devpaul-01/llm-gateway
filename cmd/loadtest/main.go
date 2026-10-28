// loadtest seeds a demo project and hammers the gateway, printing the numbers
// that matter: success rate, how many requests failover rescued, latency
// percentiles, and the cache speed-up.
//
//	go run ./cmd/loadtest seed -admin-url http://localhost:8080 -admin-token $ADMIN_TOKEN \
//	    -provider groq -provider mistral
//	go run ./cmd/loadtest run  -url http://localhost:8080 -key gw_live_... -n 500 -c 20
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: loadtest seed|run [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "seed":
		seed(os.Args[2:])
	case "run":
		run(os.Args[2:])
	default:
		fmt.Println("usage: loadtest seed|run [flags]")
		os.Exit(2)
	}
}

func adminPost(base, token, path string, body interface{}) []byte {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("%s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		log.Fatalf("%s -> %d: %s", path, resp.StatusCode, b)
	}
	return b
}

func seed(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	base := fs.String("admin-url", "http://localhost:8080", "gateway base URL")
	token := fs.String("admin-token", os.Getenv("ADMIN_TOKEN"), "admin token")
	name := fs.String("name", "demo", "project name")
	var provs listFlag
	fs.Var(&provs, "provider", "provider to add a placeholder credential for (repeatable)")
	fs.Parse(args)
	if len(provs) == 0 {
		provs = listFlag{"groq", "mistral"}
	}

	var proj struct{ ID string }
	json.Unmarshal(adminPost(*base, *token, "/admin/projects", map[string]string{"name": *name}), &proj)
	for _, p := range provs {
		adminPost(*base, *token, "/admin/credentials", map[string]string{"project_id": proj.ID, "provider": p, "label": "demo", "api_key": "demo-key"})
	}
	var key struct{ Key string }
	json.Unmarshal(adminPost(*base, *token, "/admin/gateway-keys", map[string]string{"project_id": proj.ID, "label": "demo"}), &key)
	fmt.Printf("project_id=%s\ngateway_key=%s\n", proj.ID, key.Key)
}

type result struct {
	ok       bool
	status   int
	ms       float64
	cache    string
	attempts int
	provider string
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	url := fs.String("url", "http://localhost:8080", "gateway base URL")
	key := fs.String("key", os.Getenv("GATEWAY_KEY"), "gateway API key")
	n := fs.Int("n", 200, "total requests")
	c := fs.Int("c", 10, "concurrency")
	model := fs.String("model", "llama-3.3-70b-versatile", "primary model")
	fallback := fs.String("fallback", "mistral-large-latest", "fallback model ('' for none)")
	repeat := fs.Float64("repeat", 0.5, "fraction of requests that repeat an earlier prompt (exercises the cache)")
	nonce := fs.Int64("nonce", time.Now().UnixNano()%1000000, "prompt namespace; the same value on two runs makes them share cache entries")
	fs.Parse(args)

	var fb []string
	if *fallback != "" {
		fb = []string{*fallback}
	}

	jobs := make(chan int)
	results := make([]result, *n)
	var wg sync.WaitGroup
	for w := 0; w < *c; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				prompt := fmt.Sprintf("question %d-%d", *nonce, i)
				if float64(i%100)/100 < *repeat {
					prompt = fmt.Sprintf("popular question %d-%d", *nonce, i%10)
				}
				results[i] = one(*url, *key, *model, fb, prompt)
			}
		}()
	}
	t0 := time.Now()
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(t0)

	var ok, rescued, hits int
	var lat, hitLat, missLat []float64
	byProv := map[string]int{}
	byStatus := map[int]int{}
	for _, r := range results {
		byStatus[r.status]++
		if !r.ok {
			continue
		}
		ok++
		lat = append(lat, r.ms)
		byProv[r.provider]++
		if r.cache == "hit" {
			hits++
			hitLat = append(hitLat, r.ms)
		} else {
			missLat = append(missLat, r.ms)
		}
		if r.attempts > 1 {
			rescued++
		}
	}

	pct := func(v []float64, p float64) float64 {
		if len(v) == 0 {
			return 0
		}
		sort.Float64s(v)
		return v[int(float64(len(v)-1)*p)]
	}
	fmt.Printf("\n=== %d requests, concurrency %d, %.1fs (%.0f req/s) ===\n", *n, *c, elapsed.Seconds(), float64(*n)/elapsed.Seconds())
	fmt.Printf("success rate        : %.1f%% (%d/%d)   statuses: %v\n", float64(ok)/float64(*n)*100, ok, *n, byStatus)
	fmt.Printf("rescued by failover : %d requests were served despite an upstream failure\n", rescued)
	fmt.Printf("served by           : %v\n", byProv)
	fmt.Printf("latency (all ok)    : p50 %.0fms  p95 %.0fms  p99 %.0fms\n", pct(lat, .5), pct(lat, .95), pct(lat, .99))
	fmt.Printf("cache               : %d hits (%.0f%%)   hit p50 %.0fms  vs  miss p50 %.0fms\n", hits, float64(hits)/float64(*n)*100, pct(hitLat, .5), pct(missLat, .5))
}

func one(base, key, model string, fb []string, prompt string) result {
	body, _ := json.Marshal(map[string]interface{}{
		"model": model, "fallback_models": fb, "temperature": 0, "max_tokens": 64,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	req, _ := http.NewRequest("POST", base+"/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result{status: 0}
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	r := result{status: resp.StatusCode, ms: float64(time.Since(t0).Microseconds()) / 1000, cache: resp.Header.Get("X-Gateway-Cache")}
	if resp.StatusCode == 200 {
		var out struct {
			Gateway struct {
				Provider string `json:"provider"`
				Attempts int    `json:"attempts"`
			} `json:"gateway"`
		}
		json.Unmarshal(b, &out)
		r.ok, r.provider, r.attempts = true, out.Gateway.Provider, out.Gateway.Attempts
	}
	return r
}
