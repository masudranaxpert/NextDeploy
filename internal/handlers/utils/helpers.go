package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func RandomState() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("state-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
