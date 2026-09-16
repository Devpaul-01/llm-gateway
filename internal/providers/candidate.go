package providers

type Candidate struct {
	Provider Provider
	Model    string
	Label    string // human-readable, e.g. "groq:llama-4-scout" — for logging/debugging
}

