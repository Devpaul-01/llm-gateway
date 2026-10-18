package httpserver

import (
	"encoding/json"
	"net/http"
)

// errorResponse matches the shape OpenAI-compatible clients expect to
// parse out of an error response body: {"error": {"message", "type"}}.
// Finding H-3: every error path in this package previously used
// http.Error, which writes a Content-Type: text/plain body. Most
// OpenAI-compatible SDKs parse error responses as JSON and either fail to
// extract a meaningful message from a plain-text body or throw a
// JSON-parse error outright -- directly undermining ADR-13's "zero
// client-side code changes" goal specifically on error paths (the happy
// path was already fine).
type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// writeJSONError writes a JSON-shaped error body with the given status
// code, mirroring the OpenAI error convention. errType is a short
// machine-readable category (e.g. "invalid_request_error",
// "authentication_error", "rate_limit_error", "internal_error") -- not
// the gateway's internal ErrorCategory enum, which is a different,
// lower-level taxonomy used for provider-failure classification
// (internal/providers.ErrorCategory) and is not meant to be exposed
// verbatim to HTTP clients.
func writeJSONError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error: errorBody{
			Message: message,
			Type:    errType,
		},
	})
}
