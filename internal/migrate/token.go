package migrate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func NewDownloadToken() (plain string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(plain))
	hash = hex.EncodeToString(h[:])
	return plain, hash, nil
}

func HashToken(plain string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(h[:])
}
