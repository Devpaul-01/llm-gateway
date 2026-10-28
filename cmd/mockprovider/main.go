// mockprovider is a fake OpenAI-compatible upstream for demos, benchmarks and
// failover testing. It streams a canned answer with realistic latency and can
// be switched at runtime into failure modes:
//
//	curl -X POST localhost:9001/chaos -d '{"mode":"down"}'       # every call -> 503
//	curl -X POST localhost:9001/chaos -d '{"mode":"ratelimit"}'  # every call -> 429
//	curl -X POST localhost:9001/chaos -d '{"mode":"flaky","rate":0.3}'
//	curl -X POST localhost:9001/chaos -d '{"mode":"ok"}'
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

type chaos struct {
	mu   sync.RWMutex
	Mode string  `json:"mode"`
	Rate float64 `json:"rate"`
}

func (c *chaos) get() (string, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Mode, c.Rate
}

func main() {
	addr := flag.String("addr", ":9001", "listen address")
	name := flag.String("name", "mock", "name shown in the answer text")
	firstToken := flag.Duration("first-token", 120*time.Millisecond, "latency before the first token")
	perToken := flag.Duration("per-token", 12*time.Millisecond, "delay between tokens")
	flag.Parse()

	state := &chaos{Mode: "ok"}

	http.HandleFunc("/chaos", func(w http.ResponseWriter, r *http.Request) {
		var in chaos
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		state.mu.Lock()
		state.Mode, state.Rate = in.Mode, in.Rate
		state.mu.Unlock()
		log.Printf("[%s] chaos mode -> %s (rate %.2f)", *name, in.Mode, in.Rate)
		fmt.Fprintf(w, "mode=%s\n", in.Mode)
	})

	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		mode, rate := state.get()
		switch {
		case mode == "down":
			http.Error(w, `{"error":"service unavailable"}`, http.StatusServiceUnavailable)
			return
		case mode == "ratelimit":
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		case mode == "flaky" && rand.Float64() < rate:
			http.Error(w, `{"error":"transient failure"}`, http.StatusInternalServerError)
			return
		}

		var body struct {
			Messages []struct {
				Content interface{} `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompt := ""
		if n := len(body.Messages); n > 0 {
			prompt = fmt.Sprint(body.Messages[n-1].Content)
		}

		words := strings.Fields(fmt.Sprintf("This answer came from %s. You asked: %q. It is a canned response used to demonstrate gateway routing, caching and failover.", *name, prompt))

		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		time.Sleep(*firstToken)
		for i, word := range words {
			if i > 0 {
				time.Sleep(*perToken)
			}
			tok := word
			if i < len(words)-1 {
				tok += " "
			}
			data, _ := json.Marshal(map[string]interface{}{"choices": []map[string]interface{}{{"delta": map[string]string{"content": tok}}}})
			fmt.Fprintf(w, "data: %s\n\n", data)
			f.Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,\"total_tokens\":%d}}\n\n", len(prompt)/4+8, len(words), len(prompt)/4+8+len(words))
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	})

	log.Printf("[%s] mock provider listening on %s", *name, *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
