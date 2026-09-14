package providers


type ErrorCategory int

const (
	KeyFault ErrorCategory = iota
	ProviderTransient
	BadModel
	NonRetryable
)

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