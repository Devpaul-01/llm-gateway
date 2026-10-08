package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Devpaul-01/llm-gateway/internal/providers"
)

func streamSSE(w http.ResponseWriter, chunks <-chan providers.Chunk) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}

	for chunk := range chunks {
		if chunk.Err != nil {
			// SSE error frames are correctly sent as a "data: {...}" event
			// per the SSE/OpenAI streaming convention, not as an HTTP
			// error status -- headers and a 200 status have already been
			// sent by this point, so an HTTP-level error response isn't
			// possible here regardless. This is unrelated to Finding H-3,
			// which was about non-streaming http.Error(...) calls that
			// happen before any response body is written.
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", chunk.Err.Error())
			flusher.Flush()
			return
		}

		payload := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]string{"content": chunk.Content}},
			},
		}
		data, _ := json.Marshal(payload)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if chunk.Done {
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}
}

func writeNonStreamingResponse(w http.ResponseWriter, chunks <-chan providers.Chunk) {
	var fullContent string
	var errChunk *providers.Chunk
	var modelsUsed []providers.ModelUsage

	for chunk := range chunks {
		if chunk.Err != nil {
			e := chunk
			errChunk = &e
			break
		}
		fullContent += chunk.Content
		if chunk.Done {
			modelsUsed = chunk.ModelsUsed
		}
	}

	if errChunk != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error", errChunk.Err.Error())
		return
	}

	response := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message":       map[string]string{"role": "assistant", "content": fullContent},
				"finish_reason": "stop",
			},
		},
		"models_used": modelsUsed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
