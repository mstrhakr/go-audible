package audible_test

import (
	"testing"

	"github.com/mstrhakr/go-audible"
)

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := audible.GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier failed: %v", err)
	}

	// Base64URL encoded 32 bytes = 43 characters
	if len(verifier) != 43 {
		t.Errorf("Expected verifier length 43, got %d", len(verifier))
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test-verifier-string-for-testing"
	challenge := audible.GenerateCodeChallenge(verifier)

	// Base64URL encoded SHA256 = 43 characters
	if len(challenge) != 43 {
		t.Errorf("Expected challenge length 43, got %d", len(challenge))
	}

	// Same verifier should produce same challenge
	challenge2 := audible.GenerateCodeChallenge(verifier)
	if challenge != challenge2 {
		t.Error("Same verifier should produce same challenge")
	}
}

func TestGenerateDeviceSerial(t *testing.T) {
	serial, err := audible.GenerateDeviceSerial()
	if err != nil {
		t.Fatalf("GenerateDeviceSerial failed: %v", err)
	}

	// UUID as uppercase hex = 32 characters
	if len(serial) != 32 {
		t.Errorf("Expected serial length 32, got %d", len(serial))
	}

	// Should be unique
	serial2, _ := audible.GenerateDeviceSerial()
	if serial == serial2 {
		t.Error("Generated serials should be unique")
	}
}

func TestGenerateRandomState(t *testing.T) {
	state, err := audible.GenerateRandomState()
	if err != nil {
		t.Fatalf("GenerateRandomState failed: %v", err)
	}

	// 16 bytes hex encoded = 32 characters
	if len(state) != 32 {
		t.Errorf("Expected state length 32, got %d", len(state))
	}
}

func TestAESEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32) // All zeros for testing
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("Hello, World! This is a test message for AES encryption.")

	// Encrypt
	ciphertext, err := audible.EncryptAES(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptAES failed: %v", err)
	}

	// Ciphertext should be different from plaintext
	if string(ciphertext) == string(plaintext) {
		t.Error("Ciphertext should be different from plaintext")
	}

	// Decrypt
	decrypted, err := audible.DecryptAES(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptAES failed: %v", err)
	}

	// Decrypted should match original
	if string(decrypted) != string(plaintext) {
		t.Errorf("Decrypted text doesn't match: got %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestAESInvalidKeyLength(t *testing.T) {
	key := make([]byte, 16) // Wrong length (should be 32)
	plaintext := []byte("test")

	_, err := audible.EncryptAES(plaintext, key)
	if err == nil {
		t.Error("Expected error for invalid key length")
	}
}

func TestDeriveKey(t *testing.T) {
	password := []byte("password")
	salt := []byte("salt")

	key := audible.DeriveKey(password, salt, 1000, 32)

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}

	// Same inputs should produce same key
	key2 := audible.DeriveKey(password, salt, 1000, 32)
	if string(key) != string(key2) {
		t.Error("Same inputs should produce same key")
	}

	// Different inputs should produce different key
	key3 := audible.DeriveKey(password, []byte("different-salt"), 1000, 32)
	if string(key) == string(key3) {
		t.Error("Different inputs should produce different key")
	}
}
