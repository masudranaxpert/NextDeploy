package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const settingPHPPanelCryptoKey = "php_panel_crypto_key"

func (p *Panel) phpPanelCryptoKey(ctx context.Context) ([]byte, error) {
	raw := strings.TrimSpace(p.DB.GetSetting(ctx, settingPHPPanelCryptoKey))
	if raw == "" {
		buf := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, buf); err != nil {
			return nil, err
		}
		raw = hex.EncodeToString(buf)
		if err := p.DB.SetSetting(ctx, settingPHPPanelCryptoKey, raw); err != nil {
			return nil, err
		}
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid crypto key length")
	}
	return key, nil
}

func (p *Panel) encryptPHPPanelSecret(ctx context.Context, plain string) (string, error) {
	if strings.TrimSpace(plain) == "" {
		return "", nil
	}
	key, err := p.phpPanelCryptoKey(ctx)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func (p *Panel) decryptPHPPanelSecret(ctx context.Context, sealed string) (string, error) {
	sealed = strings.TrimSpace(sealed)
	if sealed == "" {
		return "", nil
	}
	key, err := p.phpPanelCryptoKey(ctx)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
