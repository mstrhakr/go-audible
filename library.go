package audible

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Library represents the user's Audible library.
type Library struct {
	Items          []Book `json:"items"`
	TotalResults   int    `json:"total_results"`
	ResponseGroups any    `json:"response_groups,omitempty"`
}

// Book represents an audiobook in the user's library.
//
// The Audible API returns a single "asin" field that may contain different
// identifier types depending on the book. UnmarshalJSON classifies the raw
// value into the correct typed field so callers always know what they have.
// Use BestID() to get whichever identifier is available for downstream use.
type Book struct {
	// Identifier fields — only the applicable one will be populated.
	ASIN    string `json:"asin,omitempty"`     // Audible ASIN: starts with 'B', 10 chars
	ISBN10  string `json:"isbn10,omitempty"`   // ISBN-10: exactly 10 decimal digits
	ISBN13  string `json:"isbn13,omitempty"`   // ISBN-13: exactly 13 decimal digits
	OtherID string `json:"other_id,omitempty"` // Any other identifier format

	Title               string         `json:"title"`
	Subtitle            string         `json:"subtitle,omitempty"`
	Authors             []Contributor  `json:"authors"`
	Narrators           []Contributor  `json:"narrators"`
	Publisher           string         `json:"publisher_name"`
	PublisherSummary    string         `json:"publisher_summary"`
	RuntimeMinutes      int            `json:"runtime_length_min"`
	FormatType          string         `json:"format_type"`
	Language            string         `json:"language"`
	ReleaseDate         string         `json:"release_date"`
	PurchaseDate        string         `json:"purchase_date"`
	ProductImages       ProductImages  `json:"product_images"`
	Series              []SeriesInfo   `json:"series,omitempty"`
	Relationships       []Relationship `json:"relationships,omitempty"`
	Categories          []Category     `json:"category_ladders,omitempty"`
	Rating              Rating         `json:"rating,omitempty"`
	IsDownloadable      bool           `json:"is_downloadable"`
	IsReturnable        bool           `json:"is_returnable"`
	PercentComplete     float64        `json:"percent_complete"`
	ContentDeliveryType string         `json:"content_delivery_type"`
	ContentType         string         `json:"content_type"`
	IsAyce              bool           `json:"is_ayce"`
}

// BestID returns the most specific available identifier in priority order:
// Audible ASIN → ISBN-10 → ISBN-13 → other identifier.
func (b Book) BestID() string {
	if b.ASIN != "" {
		return b.ASIN
	}
	if b.ISBN10 != "" {
		return b.ISBN10
	}
	if b.ISBN13 != "" {
		return b.ISBN13
	}
	return b.OtherID
}

// Downloadable reports whether a Book is eligible for Audible download.
//
// We do not trust Audible's `is_downloadable` value (it is often false for
// actually downloadable content). Instead, evaluate based on known content
// metadata hints (content type/format/delivery type), and let smarter checks
// validate via download flow.
func (b Book) Downloadable() bool {
	// Most downloadable content on Audible is audiobook format (or product entry).
	if strings.EqualFold(b.ContentType, "audiobook") || strings.EqualFold(b.ContentType, "product") {
		return true
	}

	// Format type may also indicate an audiobook package type.
	fmtType := strings.TrimSpace(strings.ToUpper(b.FormatType))
	switch fmtType {
	case "AAX", "AAXC", "AAXA", "MP3", "AUDIBLE AUDIOBOOK", "AUDIOBOOK":
		return true
	}

	// Content delivery type hints at actual downloadable package.
	typ := strings.TrimSpace(strings.ToUpper(b.ContentDeliveryType))
	switch typ {
	case "AAX", "AAXC", "AAXA", "MP3", "DOWNLOAD":
		return true
	}

	return false
}

// CanDownload checks whether a book is actually download-capable by optionally
// validating with the download workflow (download info + minimal stream probe).
func (c *Client) CanDownload(ctx context.Context, b Book) (bool, error) {
	if !b.Downloadable() {
		return false, nil
	}

	asin := b.BestID()
	if asin == "" {
		return false, fmt.Errorf("book has no valid identifier")
	}

	info, err := c.GetDownloadInfo(ctx, asin)
	if err != nil {
		return false, err
	}

	if info == nil || info.ContentURL == "" {
		return false, fmt.Errorf("download info missing content URL")
	}

	// Probe first 100 bytes from the content URL to confirm streaming starts.
	req, err := http.NewRequestWithContext(ctx, "GET", info.ContentURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Range", "bytes=0-99")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false, fmt.Errorf("download URL probe returned status %d", resp.StatusCode)
	}

	return true, nil
}

// UnmarshalJSON implements json.Unmarshaler. The Audible API always returns the
// identifier in the "asin" field regardless of its actual type, so we classify
// the raw value here and populate only the correct typed field.
func (b *Book) UnmarshalJSON(data []byte) error {
	// Use a local struct mirroring Book's wire format but with a plain "asin"
	// capture field and no ASIN/ISBN fields, preventing recursion.
	var raw struct {
		RawID               string         `json:"asin"`
		Title               string         `json:"title"`
		Subtitle            string         `json:"subtitle,omitempty"`
		Authors             []Contributor  `json:"authors"`
		Narrators           []Contributor  `json:"narrators"`
		Publisher           string         `json:"publisher_name"`
		PublisherSummary    string         `json:"publisher_summary"`
		RuntimeMinutes      int            `json:"runtime_length_min"`
		FormatType          string         `json:"format_type"`
		Language            string         `json:"language"`
		ReleaseDate         string         `json:"release_date"`
		PurchaseDate        string         `json:"purchase_date"`
		ProductImages       ProductImages  `json:"product_images"`
		Series              []SeriesInfo   `json:"series,omitempty"`
		Relationships       []Relationship `json:"relationships,omitempty"`
		Categories          []Category     `json:"category_ladders,omitempty"`
		Rating              Rating         `json:"rating,omitempty"`
		IsDownloadable      bool           `json:"is_downloadable"`
		IsReturnable        bool           `json:"is_returnable"`
		PercentComplete     float64        `json:"percent_complete"`
		ContentDeliveryType string         `json:"content_delivery_type"`
		ContentType         string         `json:"content_type"`
		IsAyce              bool           `json:"is_ayce"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	b.Title = raw.Title
	b.Subtitle = raw.Subtitle
	b.Authors = raw.Authors
	b.Narrators = raw.Narrators
	b.Publisher = raw.Publisher
	b.PublisherSummary = raw.PublisherSummary
	b.RuntimeMinutes = raw.RuntimeMinutes
	b.FormatType = raw.FormatType
	b.Language = raw.Language
	b.ReleaseDate = raw.ReleaseDate
	b.PurchaseDate = raw.PurchaseDate
	b.ProductImages = raw.ProductImages
	b.Series = raw.Series
	b.Relationships = raw.Relationships
	b.Categories = raw.Categories
	b.Rating = raw.Rating
	b.IsDownloadable = raw.IsDownloadable
	b.IsReturnable = raw.IsReturnable
	b.PercentComplete = raw.PercentComplete
	b.ContentDeliveryType = raw.ContentDeliveryType
	b.ContentType = raw.ContentType
	b.IsAyce = raw.IsAyce

	// Initialize identifier fields before classification.
	b.ASIN, b.ISBN10, b.ISBN13, b.OtherID = "", "", "", ""
	// Classify the raw identifier into its correct typed field.
	id := strings.TrimSpace(raw.RawID)
	switch {
	case len(id) == 10 && id[0] == 'B':
		b.ASIN = id
	case len(id) == 10 && isAllDigits(id):
		b.ISBN10 = id
	case len(id) == 13 && isAllDigits(id):
		b.ISBN13 = id
	case id != "":
		b.OtherID = id
	}
	return nil
}

// isAllDigits reports whether s consists entirely of ASCII decimal digits.
func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Contributor represents an author or narrator.
type Contributor struct {
	ASIN string `json:"asin,omitempty"`
	Name string `json:"name"`
}

// ProductImages contains URLs to product images of various sizes.
type ProductImages struct {
	Image500  string `json:"500"`
	Image1024 string `json:"1024"`
	Image2400 string `json:"2400"`
}

// SeriesInfo contains information about a book's series.
type SeriesInfo struct {
	ASIN     string `json:"asin"`
	Title    string `json:"title"`
	Sequence string `json:"sequence"` // Can be "1", "1-2", etc.
	URL      string `json:"url,omitempty"`
}

// Relationship represents a relationship to other content.
type Relationship struct {
	Type  string `json:"relationship_type"`
	ASIN  string `json:"asin"`
	Title string `json:"title,omitempty"`
	Sort  string `json:"sort,omitempty"`
}

// Category represents a category in the category ladder.
type Category struct {
	Ladder []CategoryItem `json:"ladder"`
}

// CategoryItem represents a single category.
type CategoryItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Rating contains rating information.
type Rating struct {
	OverallDistribution     Distribution `json:"overall_distribution"`
	PerformanceDistribution Distribution `json:"performance_distribution"`
	StoryDistribution       Distribution `json:"story_distribution"`
}

// Distribution contains rating distribution.
type Distribution struct {
	Average    float64 `json:"average_rating"`
	NumRatings int     `json:"num_ratings"`
}

// LibraryOption is a functional option for library requests.
type LibraryOption func(*libraryOptions)

type libraryOptions struct {
	responseGroups []string
	pageSize       int
	page           int
	sortBy         string
	purchasedAfter string
}

// WithResponseGroups sets the response groups to include in the response.
func WithResponseGroups(groups ...string) LibraryOption {
	return func(o *libraryOptions) {
		o.responseGroups = groups
	}
}

// WithPageSize sets the number of items per page.
func WithPageSize(size int) LibraryOption {
	return func(o *libraryOptions) {
		o.pageSize = size
	}
}

// WithPage sets the page number (1-indexed).
func WithPage(page int) LibraryOption {
	return func(o *libraryOptions) {
		o.page = page
	}
}

// WithSortBy sets the sort order.
func WithSortBy(sortBy string) LibraryOption {
	return func(o *libraryOptions) {
		o.sortBy = sortBy
	}
}

// WithPurchasedAfter filters to books purchased after a date (ISO 8601).
func WithPurchasedAfter(date string) LibraryOption {
	return func(o *libraryOptions) {
		o.purchasedAfter = date
	}
}

// DefaultResponseGroups is the default set of response groups for library requests.
var DefaultResponseGroups = []string{
	"contributors",
	"media",
	"price",
	"product_attrs",
	"product_desc",
	"product_details",
	"product_extended_attrs",
	"product_plan_details",
	"product_plans",
	"rating",
	"sample",
	"series",
	"sku",
}

// GetLibrary retrieves the user's Audible library.
func (c *Client) GetLibrary(ctx context.Context, opts ...LibraryOption) (*Library, error) {
	// Apply options
	options := &libraryOptions{
		responseGroups: DefaultResponseGroups,
		pageSize:       50,
		page:           1,
		sortBy:         "-PurchaseDate",
	}
	for _, opt := range opts {
		opt(options)
	}

	// Build query parameters
	params := url.Values{
		"response_groups": []string{strings.Join(options.responseGroups, ",")},
		"num_results":     []string{strconv.Itoa(options.pageSize)},
		"page":            []string{strconv.Itoa(options.page)},
		"sort_by":         []string{options.sortBy},
	}

	if options.purchasedAfter != "" {
		params.Set("purchased_after", options.purchasedAfter)
	}

	// Make request
	path := "/1.0/library?" + params.Encode()
	body, err := c.doAPIRequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}

	var library Library
	if err := json.Unmarshal(body, &library); err != nil {
		return nil, fmt.Errorf("failed to unmarshal library: %w", err)
	}

	return &library, nil
}

// GetAllLibrary retrieves all books in the user's library, handling pagination.
func (c *Client) GetAllLibrary(ctx context.Context, opts ...LibraryOption) ([]Book, error) {
	var allBooks []Book
	page := 1
	pageSize := 50

	for {
		pageOpts := append(opts, WithPage(page), WithPageSize(pageSize))
		library, err := c.GetLibrary(ctx, pageOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to get library page %d: %w", page, err)
		}

		allBooks = append(allBooks, library.Items...)

		// Stop when we get a short page (fewer items than requested means
		// we've reached the end).  Also stop if TotalResults is reported and
		// we've already fetched that many.
		if len(library.Items) < pageSize {
			break
		}
		if library.TotalResults > 0 && len(allBooks) >= library.TotalResults {
			break
		}

		page++
	}

	return allBooks, nil
}

// GetBook retrieves a single book by ASIN.
func (c *Client) GetBook(ctx context.Context, asin string, opts ...LibraryOption) (*Book, error) {
	// Apply options
	options := &libraryOptions{
		responseGroups: DefaultResponseGroups,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Build query parameters
	params := url.Values{
		"response_groups": []string{strings.Join(options.responseGroups, ",")},
	}

	// Make request
	path := fmt.Sprintf("/1.0/library/%s?%s", asin, params.Encode())
	body, err := c.doAPIRequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}

	var response struct {
		Item Book `json:"item"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal book: %w", err)
	}

	return &response.Item, nil
}

// doAPIRequest performs an authenticated API request.
func (c *Client) doAPIRequest(ctx context.Context, method, path, body string) ([]byte, error) {
	// Ensure we're authenticated
	if !c.IsAuthenticated() {
		return nil, ErrNotAuthenticated
	}

	// Refresh token if needed
	if err := c.RefreshAccessToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Build full URL
	fullURL := c.apiBaseURL() + path

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Sign the request
	if err := c.signRequest(req, body); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", fmt.Sprintf("Audible/%s (iOS %s; %s)", AppVersion, OSVersion, DeviceModel))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle error responses
	switch resp.StatusCode {
	case http.StatusOK:
		return respBody, nil
	case http.StatusUnauthorized:
		return nil, ErrInvalidCredentials
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}
}
