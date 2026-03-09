package audible_test

import (
	"bytes"
	"testing"

	"github.com/mstrhakr/go-audible"
)

func TestXXTEAEncryptDecrypt(t *testing.T) {
	// Test data (must be multiple of 4 bytes and at least 8 bytes)
	data := []byte("TestMsg!") // 8 bytes = 2 uint32s
	key := []byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
	}

	// Encrypt
	encrypted, err := audible.XXTEAEncrypt(data, key)
	if err != nil {
		t.Fatalf("XXTEAEncrypt failed: %v", err)
	}

	// Encrypted should be different
	if bytes.Equal(encrypted, data) {
		t.Error("Encrypted data should be different from original")
	}

	// Same length
	if len(encrypted) != len(data) {
		t.Errorf("Encrypted length should match: got %d, want %d", len(encrypted), len(data))
	}

	// Decrypt
	decrypted, err := audible.XXTEADecrypt(encrypted, key)
	if err != nil {
		t.Fatalf("XXTEADecrypt failed: %v", err)
	}

	// Should match original
	if !bytes.Equal(decrypted, data) {
		t.Errorf("Decrypted data doesn't match: got %q, want %q", string(decrypted), string(data))
	}
}

func TestXXTEAInvalidDataLength(t *testing.T) {
	key := make([]byte, 16)
	data := []byte("abc") // 3 bytes, not multiple of 4

	_, err := audible.XXTEAEncrypt(data, key)
	if err != audible.ErrInvalidDataLength {
		t.Errorf("Expected ErrInvalidDataLength, got %v", err)
	}
}

func TestXXTEAInvalidKeyLength(t *testing.T) {
	data := []byte("test1234") // 8 bytes
	key := []byte("shortkey")  // 8 bytes, should be 16

	_, err := audible.XXTEAEncrypt(data, key)
	if err != audible.ErrInvalidKeyLength {
		t.Errorf("Expected ErrInvalidKeyLength, got %v", err)
	}
}

func TestXXTEADataTooShort(t *testing.T) {
	key := make([]byte, 16)
	data := []byte("test") // Only 4 bytes = 1 uint32, need at least 2

	_, err := audible.XXTEAEncrypt(data, key)
	if err != audible.ErrDataTooShort {
		t.Errorf("Expected ErrDataTooShort, got %v", err)
	}
}

func TestAudibleMetadataEncryption(t *testing.T) {
	// Test with the Audible-specific encryption
	data := []byte("test metadata content here!12345") // 32 bytes

	encrypted, err := audible.EncryptAudibleMetadata(data)
	if err != nil {
		t.Fatalf("EncryptAudibleMetadata failed: %v", err)
	}

	decrypted, err := audible.DecryptAudibleMetadata(encrypted)
	if err != nil {
		t.Fatalf("DecryptAudibleMetadata failed: %v", err)
	}

	if !bytes.Equal(decrypted, data) {
		t.Errorf("Decrypted doesn't match original: got %q, want %q", string(decrypted), string(data))
	}
}
