package providers

type Candidate struct {
	Adapter      Provider
	ProviderName string
	Model        string
	Label        string
	CredentialID string
}

type ModelUsage struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}