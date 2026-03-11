// Package audible provides a pure Go client for the Audible API.
//
// This library handles OAuth authentication with Amazon/Audible, device registration,
// request signing, and API access for library management and content downloads.
package audible

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Client is the main Audible API client.
type Client struct {
	marketplace Marketplace
	httpClient  *http.Client
	credentials *Credentials
	mu          sync.RWMutex

	// OAuth state
	codeVerifier string
	deviceSerial string
}

// Credentials holds the authentication credentials for the Audible API.
type Credentials struct {
	// Device registration
	DevicePrivateKey string     `json:"device_private_key"`
	ADPToken         string     `json:"adp_token"`
	DeviceInfo       DeviceInfo `json:"device_info"`

	// OAuth tokens
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`

	// Derived values
	ActivationBytes string `json:"activation_bytes,omitempty"`

	// Metadata
	CustomerID  string    `json:"customer_id"`
	Marketplace string    `json:"marketplace"`
	CreatedAt   time.Time `json:"created_at"`
}

// DeviceInfo contains device registration details.
type DeviceInfo struct {
	DeviceName         string `json:"device_name"`
	DeviceSerialNumber string `json:"device_serial_number"`
	DeviceType         string `json:"device_type"`
}

// NewClient creates a new Audible API client for the specified marketplace.
func NewClient(marketplace Marketplace) *Client {
	return &Client{
		marketplace: marketplace,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithHTTP creates a new Audible API client with a custom HTTP client.
func NewClientWithHTTP(marketplace Marketplace, httpClient *http.Client) *Client {
	return &Client{
		marketplace: marketplace,
		httpClient:  httpClient,
	}
}

// IsAuthenticated returns true if the client has valid credentials.
func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.credentials != nil && c.credentials.ADPToken != ""
}

// GetCredentials returns a copy of the current credentials.
func (c *Client) GetCredentials() *Credentials {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.credentials == nil {
		return nil
	}
	creds := *c.credentials
	return &creds
}

// SetCredentials sets the credentials for the client.
func (c *Client) SetCredentials(creds *Credentials) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.credentials = creds
}

// LoadCredentials loads credentials from a JSON file.
func (c *Client) LoadCredentials(path string) error {
	// Implementation will read and decrypt credentials file
	return fmt.Errorf("not implemented")
}

// SaveCredentials saves credentials to a JSON file.
func (c *Client) SaveCredentials(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.credentials == nil {
		return fmt.Errorf("no credentials to save")
	}
	// Implementation will encrypt and write credentials file
	return fmt.Errorf("not implemented")
}

// RefreshAccessToken refreshes the access token if it's expired or about to expire.
func (c *Client) RefreshAccessToken(ctx context.Context) error {
	c.mu.RLock()
	if c.credentials == nil {
		c.mu.RUnlock()
		return fmt.Errorf("no credentials available")
	}
	needsRefresh := time.Until(c.credentials.ExpiresAt) <= 5*time.Minute
	c.mu.RUnlock()

	if !needsRefresh {
		return nil // Token is still valid
	}

	// Perform token refresh (doRefreshToken acquires its own lock)
	return c.doRefreshToken(ctx)
}

// MarshalCredentials returns the credentials as JSON bytes.
func (c *Client) MarshalCredentials() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.credentials == nil {
		return nil, fmt.Errorf("no credentials available")
	}
	return json.Marshal(c.credentials)
}

// UnmarshalCredentials loads credentials from JSON bytes.
func (c *Client) UnmarshalCredentials(data []byte) error {
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return fmt.Errorf("failed to unmarshal credentials: %w", err)
	}
	c.mu.Lock()
	c.credentials = &creds
	c.mu.Unlock()
	return nil
}

// SetMarketplace updates the marketplace used by this client.
// This should be called before making API requests if the default marketplace
// doesn't match the account's marketplace.
func (c *Client) SetMarketplace(marketplace Marketplace) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.marketplace = marketplace
}

// Marketplace returns the current marketplace.
func (c *Client) Marketplace() Marketplace {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.marketplace
}
