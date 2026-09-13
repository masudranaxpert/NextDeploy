package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RandomHex returns a cryptographically random hex string of n bytes.
func RandomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("rnd-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func RandomState() string {
	return RandomHex(24)
}

func RandomSecret() string {
	return RandomHex(24)
}
