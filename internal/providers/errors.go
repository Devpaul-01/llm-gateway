package providers

import "net/http"

type ErrorCategory int

const (
	KeyFault ErrorCategory = iota
	ProviderTransient
	BadModel
	NonRetryable
)

func (c ErrorCategory) String() string {
	switch c {
	case KeyFault:
		return "key_fault"
	case ProviderTransient:
		return "provider_transient"
	case BadModel:
		return "bad_model"
	case NonRetryable:
		return "non_retryable"
	default:
		return "unknown"
	}
}

type ProviderError struct {
	Category ErrorCategory
	Provider string
	Model    string
	Status   int
	Cause    error

	// ClientStatus, when set, is the HTTP status the gateway should answer its
	// own caller with (e.g. 400 for an unknown model). When zero it is derived
	// from Category and Status.
	ClientStatus int
}

func (e *ProviderError) Error() string {
	if e.Provider == "" {
		return e.Cause.Error()
	}
	return e.Provider + ": " + e.Cause.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// HTTPStatus is the status code the gateway returns to its client for this error.
func (e *ProviderError) HTTPStatus() int {
	if e.ClientStatus != 0 {
		return e.ClientStatus
	}
	switch {
	case e.Category == NonRetryable && e.Status >= 400 && e.Status < 500:
		// The upstream rejected the request itself (malformed, too long...).
		return http.StatusBadRequest
	case e.Category == KeyFault:
		// Every credential was rejected or rate limited upstream.
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func clientError(cause error) *ProviderError {
	return &ProviderError{Category: NonRetryable, Provider: "", Cause: cause, ClientStatus: http.StatusBadRequest}
}

func unavailableError(cause error) *ProviderError {
	return &ProviderError{Category: NonRetryable, Provider: "", Cause: cause, ClientStatus: http.StatusServiceUnavailable}
}

func classifyStatus(status int) ErrorCategory {
	switch {
	case status == 401 || status == 402 || status == 403 || status == 429:
		// Bad key, exhausted credit, or rate limited: another credential may work.
		return KeyFault
	case status == 404:
		return BadModel
	case status == 408 || status >= 500:
		return ProviderTransient
	default:
		return NonRetryable
	}
}
