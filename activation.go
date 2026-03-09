package audible

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ActivationBytesResponse contains the activation bytes used for AAX decryption.
type ActivationBytesResponse struct {
	// ActivationBytes is the 4-byte hex string (e.g., "00a4b6c8")
	ActivationBytes string

	// Raw is the raw activation blob from the server
	Raw []byte
}

// GetActivationBytes retrieves the activation bytes for AAX file decryption.
// These bytes are device-specific and derived from the license blob.
func (c *Client) GetActivationBytes(ctx context.Context) (*ActivationBytesResponse, error) {
	c.mu.RLock()
	creds := c.credentials
	c.mu.RUnlock()

	if creds == nil {
		return nil, ErrNotAuthenticated
	}

	// Check if we have cached activation bytes
	if creds.ActivationBytes != "" {
		return &ActivationBytesResponse{
			ActivationBytes: creds.ActivationBytes,
		}, nil
	}

	// Fetch activation blob from Audible
	blob, err := c.fetchActivationBlob(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch activation blob: %w", err)
	}

	// Extract activation bytes from blob
	activationBytes, err := ExtractActivationBytes(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to extract activation bytes: %w", err)
	}

	// Cache the activation bytes
	c.mu.Lock()
	if c.credentials != nil {
		c.credentials.ActivationBytes = activationBytes
	}
	c.mu.Unlock()

	return &ActivationBytesResponse{
		ActivationBytes: activationBytes,
		Raw:             blob,
	}, nil
}

// fetchActivationBlob fetches the raw activation blob from Audible's license endpoint.
// Matches Python audible fetch_activation_sign_auth().
func (c *Client) fetchActivationBlob(ctx context.Context) ([]byte, error) {
	// Build request URL with required params
	baseURL := fmt.Sprintf("https://www.%s/license/token", c.marketplace.AudibleDomain())
	params := url.Values{
		"player_manuf": {"Audible,iPhone"},
		"action":       {"register"},
		"player_model": {"iPhone"},
	}
	fullURL := baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add signed headers
	if err := c.signRequest(req, ""); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Set required headers — response is binary, not JSON
	req.Header.Set("User-Agent", fmt.Sprintf("Audible/%s (iOS %s; %s)", AppVersion, OSVersion, DeviceModel))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ExtractActivationBytes extracts the activation bytes from a raw Audible license blob.
//
// The extraction process:
// 1. Validate the blob contains "group_id" marker
// 2. Extract the last 0x238 (568) bytes
// 3. Unpack 8 chunks of 70 bytes each (removing 6-byte padding per chunk)
// 4. Read the first 4 bytes as little-endian uint32
// 5. Convert to 8-character hex string with leading zeros
func ExtractActivationBytes(blob []byte) (string, error) {
	// Check for error markers
	if bytes.Contains(blob, []byte("BAD_LOGIN")) || bytes.Contains(blob, []byte("Whoops")) {
		return "", fmt.Errorf("activation request rejected by server")
	}

	// Validate blob contains the expected marker
	if !bytes.Contains(blob, []byte("group_id")) {
		return "", ErrInvalidActivation
	}

	// The activation data is in the last 0x238 bytes
	const activationDataSize = 0x238 // 568 bytes
	if len(blob) < activationDataSize {
		return "", fmt.Errorf("blob too small: %d bytes", len(blob))
	}

	activationData := blob[len(blob)-activationDataSize:]

	// Unpack 8 chunks: each is 70 bytes of data + 1 byte padding = 71 bytes
	// Python: struct.unpack("70s1x" * 8, data) -> 71 * 8 = 568
	const dataPerChunk = 70
	const padPerChunk = 1
	const chunkSize = dataPerChunk + padPerChunk // 71
	const numChunks = 8

	unpacked := make([]byte, 0, dataPerChunk*numChunks)
	for i := 0; i < numChunks; i++ {
		chunkStart := i * chunkSize
		unpacked = append(unpacked, activationData[chunkStart:chunkStart+dataPerChunk]...)
	}

	// The activation bytes are the first 4 bytes as little-endian uint32
	activationValue := binary.LittleEndian.Uint32(unpacked[:4])

	// Format as hex, zero-padded to 8 characters
	activationHex := fmt.Sprintf("%08x", activationValue)

	return activationHex, nil
}

// ExtractActivationBytesLegacy extracts activation bytes using the legacy method.
// This is an alternative extraction that works with older blob formats.
func ExtractActivationBytesLegacy(blob []byte) (string, error) {
	// Look for specific patterns in the blob
	// The legacy format stores the bytes differently

	// Search for the "license_response" or similar markers
	markers := []string{"license_response", "player_token", "activation_bytes"}

	for _, marker := range markers {
		idx := bytes.Index(blob, []byte(marker))
		if idx != -1 {
			// Try to extract from near this marker
			// This is a heuristic approach
			searchStart := idx
			searchEnd := min(idx+256, len(blob))
			searchRegion := blob[searchStart:searchEnd]

			// Look for an 8-character hex pattern
			hexPattern := findHexPattern(searchRegion)
			if hexPattern != "" {
				return hexPattern, nil
			}
		}
	}

	return "", ErrInvalidActivation
}

// findHexPattern searches for an 8-character hex string in the data.
func findHexPattern(data []byte) string {
	str := string(data)

	// Look for patterns like "00a4b6c8" (8 hex chars)
	for i := 0; i <= len(str)-8; i++ {
		candidate := str[i : i+8]
		if isValidHex(candidate) {
			// Validate it looks like activation bytes (not all zeros or all f's)
			if candidate != "00000000" && candidate != "ffffffff" {
				return strings.ToLower(candidate)
			}
		}
	}

	return ""
}

// isValidHex checks if a string contains only valid hex characters.
func isValidHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
