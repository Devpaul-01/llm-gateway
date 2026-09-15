package providers

import "context"

type FakeProvider struct {
	Chunks []Chunk
	Err    error
}

func (f *FakeProvider) Chat(ctx context.Context, req Request) (<-chan Chunk, error) {
	if f.Err != nil {
		return nil, f.Err
	}

	ch := make(chan Chunk)
	go func() {
		defer close(ch)
		for _, chunk := range f.Chunks {
			select {
			case <-ctx.Done():
				return
			case ch <- chunk:
			}
		}
	}()

	return ch, nil
}