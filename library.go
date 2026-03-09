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
type Book struct {
	ASIN                string         `json:"asin"`
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
		"response_groups": {strings.Join(options.responseGroups, ",")},
		"num_results":     {strconv.Itoa(options.pageSize)},
		"page":            {strconv.Itoa(options.page)},
		"sort_by":         {options.sortBy},
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
		"response_groups": {strings.Join(options.responseGroups, ",")},
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
	fullURL := c.marketplace.APIEndpoint() + path

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
