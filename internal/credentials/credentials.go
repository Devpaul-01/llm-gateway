package credentials

import (
	"time"
)

type Credential struct {
	ID        string
	ProjectID string
	Provider  string
	Label     string
	APIKey    string // decrypted, ready to use
	Status    string
	CreatedAt time.Time
}
