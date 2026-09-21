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
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for chunk := range chunks {
		if chunk.Err != nil {
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