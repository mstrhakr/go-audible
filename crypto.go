package audible

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// SignRequest creates a signed request signature for Audible API calls.
// The signature format is: SHA256withRSA({METHOD}\n{PATH}\n{DATE}\n{BODY}\n{ADP_TOKEN})
func SignRequest(privateKeyPEM string, method, path, body, adpToken string) (signature string, date string, err error) {
	// Parse the private key
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", "", fmt.Errorf("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", "", fmt.Errorf("failed to parse private key: %w (PKCS1: %v)", err2, err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", "", fmt.Errorf("key is not an RSA private key")
		}
	}

	// Format: 2024-01-15T10:30:00Z
	date = time.Now().UTC().Format("2006-01-02T15:04:05Z")

	// Create data to sign
	dataToSign := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, path, date, body, adpToken)

	// Hash the data
	hash := sha256.Sum256([]byte(dataToSign))

	// Sign with RSA PKCS#1 v1.5
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", "", fmt.Errorf("failed to sign: %w", err)
	}

	// Base64 encode the signature
	signature = base64.StdEncoding.EncodeToString(sig)

	return signature, date, nil
}

// GenerateCodeVerifier generates a PKCE code verifier (32 random bytes, base64url encoded).
func GenerateCodeVerifier() (string, error) {
	verifier := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, verifier); err != nil {
		return "", fmt.Errorf("failed to generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(verifier), nil
}

// GenerateCodeChallenge generates a PKCE code challenge from a code verifier.
func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// GenerateDeviceSerial generates a random device serial number (UUID format, uppercase hex).
func GenerateDeviceSerial() (string, error) {
	uuid := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, uuid); err != nil {
		return "", fmt.Errorf("failed to generate device serial: %w", err)
	}
	// Format as uppercase hex UUID
	return fmt.Sprintf("%X", uuid), nil
}

// DeriveKey derives an encryption key using PBKDF2.
func DeriveKey(password, salt []byte, iterations, keyLen int) []byte {
	return pbkdf2.Key(password, salt, iterations, keyLen, sha256.New)
}

// EncryptAES encrypts data using AES-256-CBC.
func EncryptAES(plaintext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Generate random IV
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	// Pad plaintext to block size (PKCS7)
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	// Encrypt
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	// Prepend IV to ciphertext
	result := make([]byte, len(iv)+len(ciphertext))
	copy(result, iv)
	copy(result[len(iv):], ciphertext)

	return result, nil
}

// DecryptAES decrypts data encrypted with AES-256-CBC.
func DecryptAES(ciphertext, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes for AES-256")
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Extract IV from beginning
	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext is not a multiple of block size")
	}

	// Decrypt
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	// Remove PKCS7 padding
	if len(plaintext) == 0 {
		return nil, fmt.Errorf("plaintext is empty")
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding > aes.BlockSize || padding == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := len(plaintext) - padding; i < len(plaintext); i++ {
		if plaintext[i] != byte(padding) {
			return nil, fmt.Errorf("invalid padding")
		}
	}

	return plaintext[:len(plaintext)-padding], nil
}

// GenerateRandomState generates a random state parameter for OAuth.
func GenerateRandomState() (string, error) {
	state := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, state); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(state), nil
}
