package audible

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	cdnPeekBytes        = 4096
	maxCDNRetryAttempts = 3
)

// DownloadInfo contains information needed to download an audiobook.
type DownloadInfo struct {
	// ContentURL is the URL to download the audiobook content.
	ContentURL string `json:"content_url"`

	// ContentType is the type of content (e.g., "AAXC", "AAX").
	ContentType string `json:"content_type"`

	// ContentMetadata contains metadata about the content.
	ContentMetadata ContentMetadata `json:"content_metadata"`

	// LicenseResponse contains the license/voucher for AAXC decryption.
	LicenseResponse *LicenseResponse `json:"license_response,omitempty"`
}

// ContentMetadata contains metadata about downloadable content.
type ContentMetadata struct {
	ContentReference ContentReference `json:"content_reference"`
	ChapterInfo      ChapterInfo      `json:"chapter_info"`
}

// ContentReference contains content identifiers.
type ContentReference struct {
	ASIN               string `json:"asin"`
	ContentSizeInBytes int64  `json:"content_size_in_bytes"`
	FileVersion        string `json:"file_version"`
	ACR                string `json:"acr"`
	ContentFormat      string `json:"content_format"`
	SkuLite            string `json:"sku_lite"`
	Version            string `json:"version"`
}

// ChapterInfo contains chapter information for the audiobook.
type ChapterInfo struct {
	BrandIntroDurationMs int       `json:"brand_intro_duration_ms"`
	BrandOutroDurationMs int       `json:"brand_outro_duration_ms"`
	RuntimeLengthMs      int       `json:"runtime_length_ms"`
	RuntimeLengthSec     int       `json:"runtime_length_sec"`
	IsAccurate           bool      `json:"is_accurate"`
	Chapters             []Chapter `json:"chapters"`
}

// Chapter represents a single chapter.
type Chapter struct {
	Title          string    `json:"title"`
	StartOffsetMs  int       `json:"start_offset_ms"`
	StartOffsetSec int       `json:"start_offset_sec"`
	LengthMs       int       `json:"length_ms"`
	Chapters       []Chapter `json:"chapters,omitempty"` // Nested chapters
}

// LicenseResponse contains the license/voucher for AAXC decryption.
type LicenseResponse struct {
	// Key is the decryption key (hex encoded).
	Key string `json:"key"`

	// IV is the initialization vector (hex encoded).
	IV string `json:"iv"`

	// Voucher is the raw voucher data.
	Voucher string `json:"voucher,omitempty"`

	// RefreshDate is when the license should be refreshed.
	RefreshDate string `json:"refresh_date,omitempty"`
}

// GetDownloadInfo retrieves the download URL and license for an audiobook.
func (c *Client) GetDownloadInfo(ctx context.Context, asin string) (*DownloadInfo, error) {
	// Request license and download URL
	params := url.Values{
		"response_groups": {"content_reference,chapter_info"},
	}

	path := fmt.Sprintf("/1.0/content/%s/licenserequest?%s", asin, params.Encode())

	// Build license request body.
	// supported_drm_types (PLURAL, array) requests AAXC when available (key+IV returned).
	// NOTE: Using "drm_type" (singular) will NOT return AAXC credentials!
	reqBody := map[string]interface{}{
		"supported_drm_types": []string{"Mpeg", "Adrm"},
		"consumption_type":    "Download",
		"quality":             "High",
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	respBody, err := c.doAPIRequest(ctx, "POST", path, string(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("license request failed: %w", err)
	}

	// DEBUG: Log raw API response to stderr for diagnostics
	fmt.Fprintf(os.Stderr, "[go-audible] RAW API RESPONSE for ASIN %s (length=%d bytes):\n%s\n\n",
		asin, len(respBody), string(respBody))

	// Parse content_license in a tolerant way because Audible can return
	// key/iv in different nested shapes depending on marketplace/content.
	var root map[string]any
	if err := json.Unmarshal(respBody, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal license response: %w", err)
	}

	contentLicenseRaw, ok := root["content_license"]
	if !ok {
		return nil, fmt.Errorf("license response missing content_license")
	}

	contentLicenseMap, ok := contentLicenseRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("license response content_license has unexpected type")
	}

	license := struct {
		ASIN            string
		ContentMetadata json.RawMessage
		ContentURL      string
		VoucherMessage  string
		StatusCode      string
		DrmType         string
		Voucher         string
		Key             string
		IV              string
		RefreshDate     string
		LicenseResponse string
	}{}

	license.ASIN = asString(contentLicenseMap["asin"])
	license.ContentURL = asString(contentLicenseMap["content_url"])
	license.VoucherMessage = asString(contentLicenseMap["message"])
	license.StatusCode = asString(contentLicenseMap["status_code"])
	license.DrmType = asString(contentLicenseMap["drm_type"])
	license.Voucher = asString(contentLicenseMap["voucher"])
	license.Key = asString(contentLicenseMap["key"])
	license.IV = asString(contentLicenseMap["iv"])
	license.RefreshDate = asString(contentLicenseMap["refresh_date"])

	if v, ok := contentLicenseMap["content_metadata"]; ok {
		if b, err := json.Marshal(v); err == nil {
			license.ContentMetadata = json.RawMessage(b)
		}
	}

	if v, ok := contentLicenseMap["license_response"]; ok {
		switch vv := v.(type) {
		case string:
			license.LicenseResponse = vv
		case map[string]any:
			if b, err := json.Marshal(vv); err == nil {
				license.LicenseResponse = string(b)
			}
		default:
			if b, err := json.Marshal(v); err == nil {
				license.LicenseResponse = string(b)
			}
		}
	}

	// Parse content_metadata into our struct
	var contentMeta ContentMetadata
	if license.ContentMetadata != nil {
		_ = json.Unmarshal(license.ContentMetadata, &contentMeta)
	}

	// Try to extract content_url from nested content_metadata.content_url.offline_url
	var nestedURL struct {
		ContentURL struct {
			OfflineURL string `json:"offline_url"`
		} `json:"content_url"`
	}
	if license.ContentMetadata != nil {
		_ = json.Unmarshal(license.ContentMetadata, &nestedURL)
	}

	// Extract key/iv from nested structures when top-level fields are empty.
	if license.Key == "" || license.IV == "" {
		if k, i := findKeyIV(contentLicenseMap); k != "" && i != "" {
			license.Key, license.IV = k, i
		}
	}
	if (license.Key == "" || license.IV == "") && license.LicenseResponse != "" {
		var lr any
		if err := json.Unmarshal([]byte(license.LicenseResponse), &lr); err == nil {
			if k, i := findKeyIV(lr); k != "" && i != "" {
				license.Key, license.IV = k, i
			}
		}
	}

	// If still no key/IV and we have an encrypted voucher, decrypt it
	if (license.Key == "" || license.IV == "") && license.LicenseResponse != "" {
		c.mu.RLock()
		creds := c.credentials
		c.mu.RUnlock()

		if creds != nil && creds.DeviceInfo.DeviceSerialNumber != "" {
			customerIDs := candidateCustomerIDs(creds.CustomerID, license.VoucherMessage)
			var lastErr error
			for _, customerID := range customerIDs {
				key, iv, err := DecryptVoucher(
					license.LicenseResponse,
					creds.DeviceInfo.DeviceType,
					creds.DeviceInfo.DeviceSerialNumber,
					customerID,
					asin,
				)
				if err != nil {
					lastErr = err
					continue
				}
				if !looksLikeAAXCKeyIV(key, iv) {
					lastErr = fmt.Errorf("decrypted voucher returned non-hex or unexpected key/iv lengths")
					continue
				}
				license.Key, license.IV = key, iv
				fmt.Fprintf(os.Stderr, "[go-audible] Successfully decrypted voucher for ASIN %s using customer_id=%s\n", asin, customerID)
				break
			}
			if license.Key == "" || license.IV == "" {
				fmt.Fprintf(os.Stderr, "[go-audible] Failed to decrypt voucher for ASIN %s after %d customer_id candidates: %v\n", asin, len(customerIDs), lastErr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[go-audible] Cannot decrypt voucher: missing device info\n")
		}
	}

	isAAXC := license.Key != "" && license.IV != ""

	// DEBUG: Log API response details to stderr for diagnostics
	debugOutput := fmt.Sprintf(
		"[go-audible] GetDownloadInfo for ASIN %s: drm_type=%s, statusCode=%s, "+
			"keyPresent=%v, ivPresent=%v, isAAXC=%v, voucherPresent=%v\n",
		asin, license.DrmType, license.StatusCode,
		license.Key != "", license.IV != "", isAAXC, license.Voucher != "")
	fmt.Fprint(os.Stderr, debugOutput)

	// If in verbose/debug mode, also log the actual keys (truncated for safety)
	if os.Getenv("DEBUG_GO_AUDIBLE") != "" {
		keyTrunc := license.Key
		if len(keyTrunc) > 16 {
			keyTrunc = keyTrunc[:8] + "..." + keyTrunc[len(keyTrunc)-8:]
		}
		ivTrunc := license.IV
		if len(ivTrunc) > 16 {
			ivTrunc = ivTrunc[:8] + "..." + ivTrunc[len(ivTrunc)-8:]
		}
		verboseOutput := fmt.Sprintf(
			"[go-audible] DEBUG: key=%s, iv=%s, licenseResponse_len=%d, voucher_len=%d\n",
			keyTrunc, ivTrunc, len(license.LicenseResponse), len(license.Voucher))
		fmt.Fprint(os.Stderr, verboseOutput)
	}

	// Determine download URL: prefer content_url sources from the license.
	downloadURL := license.ContentURL
	if downloadURL == "" {
		downloadURL = nestedURL.ContentURL.OfflineURL
	}
	if downloadURL == "" {
		// Build URL ourselves with proper codec format.
		// CDN expects specific codec strings, not bare "AAX"/"AAXC".
		codec := "AAX_44_128"
		if isAAXC {
			codec = "LC_128_44100_stereo"
		}
		downloadURL = fmt.Sprintf("https://cds.audible.%s/download?asin=%s&codec=%s&type=AUDI",
			c.marketplace.Domain, asin, codec)
	}

	result := &DownloadInfo{
		ContentURL:      downloadURL,
		ContentType:     license.DrmType,
		ContentMetadata: contentMeta,
	}

	// Include license info if AAXC
	if isAAXC || license.Voucher != "" {
		result.LicenseResponse = &LicenseResponse{
			Key:         license.Key,
			IV:          license.IV,
			Voucher:     license.Voucher,
			RefreshDate: license.RefreshDate,
		}
	}

	return result, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func candidateCustomerIDs(storedCustomerID, voucherMessage string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	add(storedCustomerID)

	if strings.HasPrefix(storedCustomerID, "amzn1.account.") {
		add(strings.TrimPrefix(storedCustomerID, "amzn1.account."))
	}

	if userID := extractUserIDFromVoucherMessage(voucherMessage); userID != "" {
		add(userID)
	}

	return out
}

func extractUserIDFromVoucherMessage(msg string) string {
	start := strings.Index(msg, "User [")
	if start < 0 {
		return ""
	}
	start += len("User [")
	endRel := strings.Index(msg[start:], "]")
	if endRel < 0 {
		return ""
	}
	return strings.TrimSpace(msg[start : start+endRel])
}

func looksLikeAAXCKeyIV(key, iv string) bool {
	if len(key) != 32 || len(iv) != 32 {
		return false
	}
	return isHex(key) && isHex(iv)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

type keyIVState struct {
	key string
	iv  string
}

func findKeyIV(v any) (string, string) {
	st := &keyIVState{}
	walkForKeyIV(v, st)
	return st.key, st.iv
}

func walkForKeyIV(v any, st *keyIVState) {
	if st.key != "" && st.iv != "" {
		return
	}

	switch node := v.(type) {
	case map[string]any:
		if st.key == "" {
			if val, ok := node["key"].(string); ok && val != "" {
				st.key = val
			}
		}
		if st.iv == "" {
			if val, ok := node["iv"].(string); ok && val != "" {
				st.iv = val
			}
		}
		for _, child := range node {
			walkForKeyIV(child, st)
			if st.key != "" && st.iv != "" {
				return
			}
		}
	case []any:
		for _, child := range node {
			walkForKeyIV(child, st)
			if st.key != "" && st.iv != "" {
				return
			}
		}
	case string:
		trimmed := strings.TrimSpace(node)
		if !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
			return
		}
		var nested any
		if err := json.Unmarshal([]byte(trimmed), &nested); err == nil {
			walkForKeyIV(nested, st)
		}
	}
}

// GetChapters retrieves chapter information for an audiobook.
func (c *Client) GetChapters(ctx context.Context, asin string) (*ChapterInfo, error) {
	params := url.Values{
		"response_groups": {"chapter_info"},
	}

	path := fmt.Sprintf("/1.0/content/%s/metadata?%s", asin, params.Encode())

	respBody, err := c.doAPIRequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}

	var response struct {
		ContentMetadata struct {
			ChapterInfo ChapterInfo `json:"chapter_info"`
		} `json:"content_metadata"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chapters: %w", err)
	}

	return &response.ContentMetadata.ChapterInfo, nil
}

// DownloadBook downloads an audiobook to the specified writer.
// Returns the number of bytes written.
func (c *Client) DownloadBook(ctx context.Context, asin string, writer DownloadWriter) (int64, error) {
	for attempt := 1; attempt <= maxCDNRetryAttempts; attempt++ {
		// Get a fresh content URL each attempt. Some CDN responses are transient.
		info, err := c.GetDownloadInfo(ctx, asin)
		if err != nil {
			return 0, fmt.Errorf("failed to get download info: %w", err)
		}

		if info.ContentURL == "" {
			return 0, fmt.Errorf("no content URL available for %s", asin)
		}

		written, err := c.downloadBookAttempt(ctx, asin, writer, info)
		if err == nil {
			return written, nil
		}

		if !isRetryableCDNError(err) || attempt == maxCDNRetryAttempts {
			if attempt > 1 {
				return 0, fmt.Errorf("download failed after %d attempts: %w", attempt, err)
			}
			return 0, err
		}
	}

	return 0, fmt.Errorf("download failed after %d attempts", maxCDNRetryAttempts)
}

func (c *Client) downloadBookAttempt(ctx context.Context, asin string, writer DownloadWriter, info *DownloadInfo) (int64, error) {
	resp, err := c.doDownloadRequest(ctx, info.ContentURL)
	if err != nil {
		return 0, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Peek at the first bytes to detect short textual CDN error responses.
	peekBuf := make([]byte, cdnPeekBytes)
	peekN, peekErr := io.ReadFull(resp.Body, peekBuf)
	if peekN > 0 && peekN < cdnPeekBytes {
		errText := strings.TrimSpace(string(peekBuf[:peekN]))
		return 0, &cdnTextError{peekBytes: peekN, message: errText}
	}
	if peekErr != nil && peekN == 0 {
		return 0, fmt.Errorf("failed to read CDN response: %w", peekErr)
	}

	fullBody := io.MultiReader(bytes.NewReader(peekBuf[:peekN]), resp.Body)
	contentLength := resp.ContentLength

	if err := writer.OnStart(asin, contentLength, info); err != nil {
		return 0, fmt.Errorf("download writer OnStart failed: %w", err)
	}

	buf := make([]byte, 32*1024)
	var written int64

	for {
		n, readErr := fullBody.Read(buf)
		if n > 0 {
			nw, werr := writer.Write(buf[:n])
			if werr != nil {
				return written, fmt.Errorf("write error: %w", werr)
			}
			written += int64(nw)

			if perr := writer.OnProgress(written, contentLength); perr != nil {
				return written, fmt.Errorf("progress callback error: %w", perr)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return written, fmt.Errorf("read error: %w", readErr)
		}
	}

	if err := writer.OnComplete(); err != nil {
		return written, fmt.Errorf("download writer OnComplete failed: %w", err)
	}

	return written, nil
}

type cdnTextError struct {
	peekBytes int
	message   string
}

func (e *cdnTextError) Error() string {
	return fmt.Sprintf("CDN returned error instead of audio (%d bytes): %s", e.peekBytes, e.message)
}

func isRetryableCDNError(err error) bool {
	var cdnErr *cdnTextError
	if !errors.As(err, &cdnErr) {
		return false
	}

	msg := strings.ToLower(cdnErr.message)
	return strings.Contains(msg, "file assembly error") && strings.Contains(msg, "invalid audio format")
}

// DownloadWriter is the interface for receiving downloaded content.
type DownloadWriter interface {
	// OnStart is called when the download begins.
	OnStart(asin string, contentLength int64, info *DownloadInfo) error

	// Write receives chunks of downloaded data.
	Write(p []byte) (n int, err error)

	// OnProgress is called periodically with download progress.
	OnProgress(bytesWritten, totalBytes int64) error

	// OnComplete is called when the download is finished.
	OnComplete() error
}

// FormatChaptersFile generates a chapters.txt file content for Plex.
func FormatChaptersFile(chapters []Chapter) string {
	var sb strings.Builder

	for _, ch := range chapters {
		// Convert milliseconds to HH:MM:SS.mmm format
		ms := ch.StartOffsetMs
		hours := ms / 3600000
		ms %= 3600000
		minutes := ms / 60000
		ms %= 60000
		seconds := ms / 1000
		millis := ms % 1000

		sb.WriteString(fmt.Sprintf("%02d:%02d:%02d.%03d %s\n", hours, minutes, seconds, millis, ch.Title))
	}

	return sb.String()
}

// doDownloadRequest performs an authenticated GET request to the Audible CDN.
// The CDN authenticates via access_token (not ADP signing which is for the API).
// Uses a dedicated HTTP client without a timeout since audiobook downloads can
// take many minutes for large files.
func (c *Client) doDownloadRequest(ctx context.Context, downloadURL string) (*http.Response, error) {
	if !c.IsAuthenticated() {
		return nil, ErrNotAuthenticated
	}

	if err := c.RefreshAccessToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	c.mu.RLock()
	accessToken := c.credentials.AccessToken
	c.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf("Audible/%s (iOS %s; %s)", AppVersion, OSVersion, DeviceModel))
	req.Header.Set("Accept", "audio/mpeg, audio/x-m4a, audio/aax, */*")

	// CDN authenticates via Bearer token, not ADP signing
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	// Also sign with ADP in case the CDN accepts it
	_ = c.signRequest(req, "")

	// Use a client with no timeout — audiobook files can be hundreds of MB.
	// The caller controls cancellation via the context.
	dlClient := &http.Client{
		Transport: c.httpClient.Transport,
	}
	return dlClient.Do(req)
}
