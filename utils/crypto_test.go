package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptDecryptSecretAESGCM(t *testing.T) {
	SetSecretKeyFile(filepath.Join(t.TempDir(), "master.key"))

	encrypted, err := EncryptSecret("sk-test-secret")
	if err != nil {
		t.Fatalf("EncryptSecret returned error: %v", err)
	}
	if encrypted == "sk-test-secret" {
		t.Fatal("EncryptSecret returned plaintext")
	}
	if !strings.HasPrefix(encrypted, encryptedPrefix) {
		t.Fatalf("EncryptSecret prefix = %q", encrypted)
	}

	decrypted, err := DecryptSecret(encrypted)
	if err != nil {
		t.Fatalf("DecryptSecret returned error: %v", err)
	}
	if decrypted != "sk-test-secret" {
		t.Fatalf("DecryptSecret = %q", decrypted)
	}
}

func TestDecryptLegacyBase64Secret(t *testing.T) {
	decrypted, err := DecryptSecret("c2stbGVnYWN5")
	if err != nil {
		t.Fatalf("DecryptSecret returned error: %v", err)
	}
	if decrypted != "sk-legacy" {
		t.Fatalf("DecryptSecret legacy = %q", decrypted)
	}
}
