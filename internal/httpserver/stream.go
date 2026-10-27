package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Devpaul-01/llm-gateway/internal/providers"
)

// gatewayMeta is the gateway-specific block added to responses: it reports how
// the request was served (timing, cache, failover) without breaking the
// OpenAI response schema, which clients ignore unknown fields of.
type gatewayMeta struct {
	RequestID      string `json:"request_id"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Cache          string `json:"cache"`
	Attempts       int    `json:"attempts"`
	Failovers      int    `json:"failovers"`
	TTFTMs         *int   `json:"ttft_ms"`
	TotalMs        int    `json:"total_ms"`
	UsageEstimated bool   `json:"usage_estimated"`
}

func metaFrom(s *providers.Summary, cache string) gatewayMeta {
	return gatewayMeta{
		RequestID:      s.RequestID,
		Provider:       s.Provider,
		Model:          s.Model,
		Cache:          cache,
		Attempts:       s.Attempts,
		Failovers:      s.Failovers,
		TTFTMs:         s.TTFTMs,
		TotalMs:        s.TotalMs,
		UsageEstimated: s.UsageEstimated,
	}
}

// chunkReader lets a handler look at the first chunk (to turn an early failure
// into a real HTTP error status) and then carry on reading the same stream.
type chunkReader struct {
	first   *providers.Chunk
	ch      <-chan providers.Chunk
	started bool
}

func (r *chunkReader) Next() (providers.Chunk, bool) {
	if !r.started {
		r.started = true
		if r.first != nil {
			return *r.first, true
		}
	}
	c, ok := <-r.ch
	return c, ok
}

func newChunkReader(ch <-chan providers.Chunk) *chunkReader {
	r := &chunkReader{ch: ch}
	if c, ok := <-ch; ok {
		r.first = &c
	}
	return r
}

func statusForChunkErr(e *providers.ProviderError) int {
	return e.HTTPStatus()
}

func completionID(requestID string) string {
	return "chatcmpl-" + strings.TrimPrefix(requestID, "req_")
}

func setCommonHeaders(w http.ResponseWriter, requestID, cache string) {
	w.Header().Set("X-Gateway-Request-Id", requestID)
	w.Header().Set("X-Gateway-Cache", cache)
}

type streamResult struct {
	Summary *providers.Summary
	// StreamedChars counts content written to the client; used to settle the
	// token budget when a stream ends without a final summary (disconnects).
	StreamedChars int
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

type streamChoice struct {
	Index        int               `json:"index"`
	Delta        map[string]string `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type streamEnvelope struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []streamChoice   `json:"choices"`
	Usage   *providers.Usage `json:"usage,omitempty"`
	Gateway *gatewayMeta     `json:"gateway,omitempty"`
}

func streamSSE(w http.ResponseWriter, cr *chunkReader, requestID, cache string, includeUsage bool, fallbackModel string) streamResult {
	var res streamResult

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "no_streaming", "streaming not supported")
		return res
	}

	// Peek: if the very first thing is an error nothing has been sent yet, so
	// the client gets a proper HTTP status instead of a 200 with an error inside.
	if cr.first != nil && cr.first.Err != nil {
		writeAPIError(w, statusForChunkErr(cr.first.Err), "upstream_failed", cr.first.Err.Error())
		return res
	}
	if cr.first == nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_failed", "no response from any provider")
		return res
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // stop nginx-style proxies from buffering the stream
	setCommonHeaders(w, requestID, cache)

	id := completionID(requestID)
	created := time.Now().Unix()
	model := fallbackModel
	sentRole := false

	for {
		chunk, ok := cr.Next()
		if !ok {
			return res
		}

		if chunk.Err != nil {
			errJSON, _ := json.Marshal(apiErrorBody{Error: apiErrorDetail{Message: chunk.Err.Error(), Type: errorTypeFor(statusForChunkErr(chunk.Err)), Code: "stream_interrupted"}})
			fmt.Fprintf(w, "data: %s\n\n", errJSON)
			flusher.Flush()
			return res
		}

		if chunk.Model != "" {
			model = chunk.Model
		}

		if chunk.Done {
			res.Summary = chunk.Summary
			stop := chunk.FinishReason
			if stop == "" {
				stop = "stop"
			}
			meta := gatewayMeta{}
			if chunk.Summary != nil {
				meta = metaFrom(chunk.Summary, cache)
			}
			writeSSE(w, flusher, streamEnvelope{
				ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []streamChoice{{Index: 0, Delta: map[string]string{}, FinishReason: &stop}},
				Gateway: &meta,
			})
			if includeUsage && chunk.Usage != nil {
				writeSSE(w, flusher, streamEnvelope{
					ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
					Choices: []streamChoice{}, Usage: chunk.Usage, Gateway: &meta,
				})
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return res
		}

		delta := map[string]string{"content": chunk.Content}
		if !sentRole {
			delta["role"] = "assistant"
			sentRole = true
		}
		res.StreamedChars += len(chunk.Content)
		writeSSE(w, flusher, streamEnvelope{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: model,
			Choices: []streamChoice{{Index: 0, Delta: delta}},
		})
	}
}

type completionChoice struct {
	Index        int               `json:"index"`
	Message      map[string]string `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type completionResponse struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object"`
	Created    int64                  `json:"created"`
	Model      string                 `json:"model"`
	Choices    []completionChoice     `json:"choices"`
	Usage      providers.Usage        `json:"usage"`
	ModelsUsed []providers.ModelUsage `json:"models_used"`
	Gateway    gatewayMeta            `json:"gateway"`
}

func writeNonStreamingResponse(w http.ResponseWriter, cr *chunkReader, requestID, cache string) streamResult {
	var res streamResult
	var content strings.Builder
	var final *providers.Chunk

	for {
		chunk, ok := cr.Next()
		if !ok {
			break
		}
		if chunk.Err != nil {
			res.StreamedChars = content.Len()
			writeAPIError(w, statusForChunkErr(chunk.Err), "upstream_failed", chunk.Err.Error())
			return res
		}
		content.WriteString(chunk.Content)
		if chunk.Done {
			c := chunk
			final = &c
			break
		}
	}
	res.StreamedChars = content.Len()

	if final == nil || final.Summary == nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_failed", "provider returned no completion")
		return res
	}
	res.Summary = final.Summary
	s := final.Summary
	meta := metaFrom(s, cache)

	// Timing and routing info is also exposed as headers so it is visible to
	// proxies, curl -i, and browsers' devtools without parsing the body.
	h := w.Header()
	h.Set("Content-Type", "application/json")
	setCommonHeaders(w, requestID, cache)
	h.Set("X-Gateway-Provider", s.Provider)
	h.Set("X-Gateway-Model", s.Model)
	h.Set("X-Gateway-Attempts", strconv.Itoa(s.Attempts))
	timing := "total;dur=" + strconv.Itoa(s.TotalMs)
	if s.TTFTMs != nil {
		timing += ", ttft;dur=" + strconv.Itoa(*s.TTFTMs)
	}
	h.Set("Server-Timing", timing)

	_ = json.NewEncoder(w).Encode(completionResponse{
		ID:      completionID(requestID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   s.Model,
		Choices: []completionChoice{{
			Index:        0,
			Message:      map[string]string{"role": "assistant", "content": content.String()},
			FinishReason: s.FinishReason,
		}},
		Usage:      s.Usage,
		ModelsUsed: s.ModelsUsed,
		Gateway:    meta,
	})
	return res
}
