package httpserver

import (
	"encoding/json"
	"net/http"
)

type apiErrorBody struct {
	Error apiErrorDetail `json:"error"`
}

type apiErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func errorTypeFor(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusInternalServerError:
		return "internal_error"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusBadGateway:
		return "upstream_error"
	default:
		return "api_error"
	}
}

// writeAPIError replies in the same JSON shape OpenAI clients already parse,
// so SDKs surface a readable message instead of choking on a text/plain body.
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorBody{Error: apiErrorDetail{Message: message, Type: errorTypeFor(status), Code: code}})
}
