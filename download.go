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
	// quality=High + drm_type=Adrm requests AAXC when available (key+IV returned).
	reqBody := map[string]interface{}{
		"drm_type":         "Adrm",
		"consumption_type": "Download",
		"quality":          "High",
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	respBody, err := c.doAPIRequest(ctx, "POST", path, string(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("license request failed: %w", err)
	}

	// Use json.RawMessage for content_metadata so we can parse nested content_url
	var response struct {
		ContentLicense struct {
			ASIN            string          `json:"asin"`
			ContentMetadata json.RawMessage `json:"content_metadata"`
			ContentURL      string          `json:"content_url"`
			VoucherMessage  string          `json:"message,omitempty"`
			StatusCode      string          `json:"status_code"`
			DrmType         string          `json:"drm_type"`
			Voucher         string          `json:"voucher,omitempty"`
			Key             string          `json:"key,omitempty"`
			IV              string          `json:"iv,omitempty"`
			RefreshDate     string          `json:"refresh_date,omitempty"`
			LicenseResponse string          `json:"license_response,omitempty"`
		} `json:"content_license"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal license response: %w", err)
	}

	license := response.ContentLicense

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

	// Try to extract key/iv from the nested license_response JSON string
	// if the flat fields are empty. Some content returns them embedded.
	if license.Key == "" && license.IV == "" && license.LicenseResponse != "" {
		var nested struct {
			Key string `json:"key"`
			IV  string `json:"iv"`
		}
		if err := json.Unmarshal([]byte(license.LicenseResponse), &nested); err == nil {
			license.Key = nested.Key
			license.IV = nested.IV
		}
	}

	isAAXC := license.Key != "" && license.IV != ""

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
