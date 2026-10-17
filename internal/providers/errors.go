package providers

type ErrorCategory int

const (
	KeyFault ErrorCategory = iota
	ProviderTransient
	BadModel
	NonRetryable
)

// String renders the category as the lowercase snake_case form stored in
// request_logs.error_category (Finding B-1's fix). Kept as an explicit
// method rather than relying on the int's default formatting so the
// on-disk representation is stable and human-readable regardless of how
// the iota ordering above might change in the future.
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
}

func (e *ProviderError) Error() string {
	return e.Provider + ": " + e.Cause.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}