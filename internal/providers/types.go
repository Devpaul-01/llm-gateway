package providers

type Message struct {
	Role    string
	Content string
	Images  []Image
}

type Image struct {
	URL string
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type Request struct {
	Messages          []Message
	Model             string
	Temperature       float64
	Provider          string
	MaxTokens         int
	ContinueOnFailure bool
}

type Chunk struct {
	Content    string
	Done       bool
	Usage      *Usage
	Err        *ProviderError
	ModelsUsed []ModelUsage
}
