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