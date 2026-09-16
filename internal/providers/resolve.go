package providers

func resolveCandidates(req Request) ([]Candidate, error) {
	return []Candidate{
		{
			Provider: &FakeProvider{
				Chunks: []Chunk{{Content: "stub response"}, {Done: true}},
			},
			Model: "stub-model-a",
			Label: "stub:model-a",
		},
	}, nil

}
