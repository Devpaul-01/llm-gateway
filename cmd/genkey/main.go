// Command genkey generates a random 32-byte hex-encoded key suitable for
// use as ENCRYPTION_KEY. Run with: go run ./cmd/genkey
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func main() {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	fmt.Println(hex.EncodeToString(key))
}
