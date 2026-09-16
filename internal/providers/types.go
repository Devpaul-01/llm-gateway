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
	Messages    []Message
	Model       string
	Temperature float64
	MaxTokens   int
}

type Chunk struct {
	Usage   *Usage
	Content string
	Done    bool
	Err     *ProviderError
}
