package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const encryptedPrefix = "aesgcm:"

var secretKeyState = struct {
	sync.Mutex
	file string
	key  []byte
}{
	file: "./data/master.key",
}

func SetSecretKeyFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	secretKeyState.Lock()
	defer secretKeyState.Unlock()
	if secretKeyState.file != path {
		secretKeyState.file = path
		secretKeyState.key = nil
	}
}

func EncryptSecret(value string) (string, error) {
	key, err := masterKey()
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
	ciphertext := gcm.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, ciphertext...)
	return encryptedPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecryptSecret(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return decryptLegacyBase64(value)
	}
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
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
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("encrypted secret payload is too short")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func masterKey() ([]byte, error) {
	secretKeyState.Lock()
	defer secretKeyState.Unlock()
	if len(secretKeyState.key) == 32 {
		return secretKeyState.key, nil
	}
	raw, err := os.ReadFile(secretKeyState.file)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		raw = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(secretKeyState.file), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(secretKeyState.file, []byte(base64.RawURLEncoding.EncodeToString(raw)), 0600); err != nil {
			return nil, err
		}
	} else {
		raw = []byte(strings.TrimSpace(string(raw)))
		if decoded, decodeErr := base64.RawURLEncoding.DecodeString(string(raw)); decodeErr == nil && len(decoded) > 0 {
			raw = decoded
		}
	}
	sum := sha256.Sum256(raw)
	secretKeyState.key = sum[:]
	return secretKeyState.key, nil
}

func decryptLegacyBase64(value string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return value, nil
	}
	return string(b), nil
}
