package audible

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AuthURL represents an OAuth authorization URL with associated state.
type AuthURL struct {
	// URL is the full authorization URL to redirect the user to.
	URL string

	// CodeVerifier is the PKCE code verifier (needed for device registration).
	CodeVerifier string

	// DeviceSerial is the device serial (needed for device registration).
	DeviceSerial string
}

// buildClientID generates the client_id from a device serial.
// Matches Python audible: (serial + "#" + DEVICE_TYPE_ID).encode().hex()
func buildClientID(serial string) string {
	raw := serial + "#" + DeviceTypeID
	return hex.EncodeToString([]byte(raw))
}

// buildInitCookies generates the initial cookies needed to reduce captcha risk.
// Based on Python audible login.py build_init_cookies().
func buildInitCookies(domain string) []*http.Cookie {
	// frc: base64 of 313 random bytes
	frcBytes := make([]byte, 313)
	_, _ = io.ReadFull(rand.Reader, frcBytes)
	frc := base64.StdEncoding.EncodeToString(frcBytes)

	// map-md: base64 of JSON config
	mapMD := map[string]interface{}{
		"device_user_dictionary": []interface{}{},
		"device_registration_data": map[string]string{
			"software_version": SoftwareVersion,
		},
		"app_identifier": map[string]string{
			"app_version": AppVersion,
			"bundle_id":   "com.audible.iphone",
		},
	}
	mapMDJSON, _ := json.Marshal(mapMD)
	mapMDEncoded := base64.StdEncoding.EncodeToString(mapMDJSON)

	return []*http.Cookie{
		{Name: "frc", Value: frc, Domain: "." + domain},
		{Name: "map-md", Value: mapMDEncoded, Domain: "." + domain},
		{Name: "amzn-app-id", Value: "MAPiOSLib/6.0/ToHideRetailLink", Domain: "." + domain},
	}
}

// parseExpiresIn handles expires_in being either a JSON number or string.
func parseExpiresIn(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	default:
		return 3600 // safe default
	}
}

// GetAuthURL generates an OAuth authorization URL for the user to visit.
// After the user signs in, Amazon redirects to a maplanding page whose URL
// contains the authorization code.  The user must copy that URL and pass it
// to [HandleAuthRedirect] to extract the code.
func (c *Client) GetAuthURL() (*AuthURL, error) {
	// Generate PKCE code verifier and challenge
	codeVerifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := GenerateCodeChallenge(codeVerifier)

	// Generate device serial
	deviceSerial, err := GenerateDeviceSerial()
	if err != nil {
		return nil, fmt.Errorf("failed to generate device serial: %w", err)
	}

	// Build client ID (hex encoded serial#device_type)
	clientID := buildClientID(deviceSerial)

	returnTo := fmt.Sprintf("https://www.%s/ap/maplanding", c.marketplace.AmazonDomain())

	// Build OAuth URL parameters — matches Python audible login.py build_oauth_url()
	params := url.Values{
		"openid.oa2.response_type":         {"code"},
		"openid.oa2.code_challenge_method": {"S256"},
		"openid.oa2.code_challenge":        {codeChallenge},
		"openid.return_to":                 {returnTo},
		"openid.assoc_handle":              {"amzn_audible_ios_" + c.marketplace.CountryCode},
		"openid.identity":                  {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id":                {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.mode":                      {"checkid_setup"},
		"openid.ns":                        {"http://specs.openid.net/auth/2.0"},
		"openid.ns.oa2":                    {"http://www.amazon.com/ap/ext/oauth/2"},
		"openid.oa2.client_id":             {"device:" + clientID},
		"openid.oa2.scope":                 {"device_auth_access"},
		"openid.ns.pape":                   {"http://specs.openid.net/extensions/pape/1.0"},
		"openid.pape.max_auth_age":         {"0"},
		"accountStatusPolicy":              {"P1"},
		"pageId":                           {"amzn_audible_ios"},
		"forceMobileLayout":                {"true"},
		"marketPlaceId":                    {c.marketplace.MarketplaceID},
	}

	// Build the full URL
	authURL := fmt.Sprintf("https://www.%s/ap/signin?%s", c.marketplace.AmazonDomain(), params.Encode())

	// Store state on client for later use
	c.mu.Lock()
	c.codeVerifier = codeVerifier
	c.deviceSerial = deviceSerial
	c.mu.Unlock()

	return &AuthURL{
		URL:          authURL,
		CodeVerifier: codeVerifier,
		DeviceSerial: deviceSerial,
	}, nil
}

// HandleAuthRedirect extracts the authorization code from the redirect URL
// returned by Amazon after the user logs in.
// The redirectURL should be the full URL the browser was redirected to
// (starts with https://www.amazon.{domain}/ap/maplanding?...).
func HandleAuthRedirect(redirectURL string) (string, error) {
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect URL: %w", err)
	}

	code := parsed.Query().Get("openid.oa2.authorization_code")
	if code == "" {
		return "", fmt.Errorf("no authorization code found in redirect URL")
	}

	return code, nil
}

// DeviceRegistrationRequest contains the parameters for device registration.
type DeviceRegistrationRequest struct {
	// AuthorizationCode is the code received from the OAuth redirect URL.
	AuthorizationCode string

	// CodeVerifier is the PKCE code verifier used when generating the auth URL.
	CodeVerifier string

	// DeviceSerial is the device serial used when generating the auth URL.
	DeviceSerial string
}

// DeviceRegistrationResponse contains the response from device registration.
type deviceRegistrationResponse struct {
	Response struct {
		Success struct {
			Extensions struct {
				DeviceInfo struct {
					DeviceName         string `json:"device_name"`
					DeviceSerialNumber string `json:"device_serial_number"`
					DeviceType         string `json:"device_type"`
				} `json:"device_info"`
				CustomerInfo struct {
					AccountPool string `json:"account_pool"`
					UserID      string `json:"user_id"`
					HomeRegion  string `json:"home_region"`
					Name        string `json:"name"`
					GivenName   string `json:"given_name"`
				} `json:"customer_info"`
			} `json:"extensions"`
			Tokens struct {
				Bearer struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
					ExpiresIn    any    `json:"expires_in"`
				} `json:"bearer"`
				MACDms struct {
					DevicePrivateKey string `json:"device_private_key"`
					ADPToken         string `json:"adp_token"`
				} `json:"mac_dms"`
				WebsiteCookies []struct {
					Name  string `json:"Name"`
					Value string `json:"Value"`
				} `json:"website_cookies"`
			} `json:"tokens"`
		} `json:"success"`
	} `json:"response"`
	RequestID string `json:"request_id"`
}

// Authenticate exchanges an authorization code for credentials via device registration.
// Uses the same DeviceSerial and CodeVerifier that were used in GetAuthURL.
func (c *Client) Authenticate(ctx context.Context, req DeviceRegistrationRequest) error {
	// Use the serial from the auth URL generation (must match)
	deviceSerial := req.DeviceSerial
	codeVerifier := req.CodeVerifier
	if deviceSerial == "" || codeVerifier == "" {
		// Fall back to stored values from GetAuthURL
		c.mu.RLock()
		if deviceSerial == "" {
			deviceSerial = c.deviceSerial
		}
		if codeVerifier == "" {
			codeVerifier = c.codeVerifier
		}
		c.mu.RUnlock()
	}
	if deviceSerial == "" {
		return fmt.Errorf("no device serial: call GetAuthURL first or set DeviceSerial in request")
	}
	if codeVerifier == "" {
		return fmt.Errorf("no code verifier: call GetAuthURL first or set CodeVerifier in request")
	}

	// Build client ID (must match auth URL)
	clientID := buildClientID(deviceSerial)

	// Build registration request body — matches Python audible register.py
	regBody := map[string]interface{}{
		"requested_token_type": []string{
			"bearer",
			"mac_dms",
			"website_cookies",
		},
		"cookies": map[string]interface{}{
			"website_cookies": []interface{}{},
			"domain":          "." + c.marketplace.AmazonDomain(),
		},
		"registration_data": map[string]interface{}{
			"domain":           "Device",
			"app_version":      AppVersion,
			"device_serial":    deviceSerial,
			"device_type":      DeviceTypeID,
			"device_name":      "%FIRST_NAME%%FIRST_NAME_POSSESSIVE_STRING%%DUPE_STRATEGY_1ST%Audible for iPhone",
			"os_version":       OSVersion,
			"software_version": SoftwareVersion,
			"device_model":     DeviceModel,
			"app_name":         AppName,
		},
		"auth_data": map[string]interface{}{
			"authorization_code": req.AuthorizationCode,
			"code_verifier":      codeVerifier,
			"code_algorithm":     "SHA-256",
			"client_domain":      "DeviceLegacy",
			"client_id":          clientID,
		},
		"requested_extensions": []string{
			"device_info",
			"customer_info",
		},
	}

	bodyJSON, err := json.Marshal(regBody)
	if err != nil {
		return fmt.Errorf("failed to marshal registration body: %w", err)
	}

	// Build request
	regURL := fmt.Sprintf("https://api.%s/auth/register", c.marketplace.AmazonDomain())
	httpReq, err := http.NewRequestWithContext(ctx, "POST", regURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", fmt.Sprintf("AmazonWebView/Audible/%s/iOS/%s/%s", AppVersion, OSVersion, DeviceModel))
	httpReq.Header.Set("x-amzn-identity-auth-domain", "api."+c.marketplace.AmazonDomain())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var regResp deviceRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return fmt.Errorf("failed to decode registration response: %w", err)
	}

	// Extract credentials
	success := regResp.Response.Success
	tokens := success.Tokens

	// Parse expires_in which can be string or number
	expiresIn := parseExpiresIn(tokens.Bearer.ExpiresIn)

	creds := &Credentials{
		DevicePrivateKey: tokens.MACDms.DevicePrivateKey,
		ADPToken:         tokens.MACDms.ADPToken,
		AccessToken:      tokens.Bearer.AccessToken,
		RefreshToken:     tokens.Bearer.RefreshToken,
		ExpiresAt:        time.Now().Add(time.Duration(expiresIn) * time.Second),
		CustomerID:       success.Extensions.CustomerInfo.UserID,
		Marketplace:      c.marketplace.CountryCode,
		CreatedAt:        time.Now(),
		DeviceInfo: DeviceInfo{
			DeviceName:         success.Extensions.DeviceInfo.DeviceName,
			DeviceSerialNumber: success.Extensions.DeviceInfo.DeviceSerialNumber,
			DeviceType:         success.Extensions.DeviceInfo.DeviceType,
		},
	}

	c.mu.Lock()
	c.credentials = creds
	c.mu.Unlock()

	return nil
}

// signRequest adds authentication headers to an HTTP request.
func (c *Client) signRequest(req *http.Request, body string) error {
	c.mu.RLock()
	creds := c.credentials
	c.mu.RUnlock()

	if creds == nil {
		return ErrNotAuthenticated
	}

	// Get the request path
	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	// Sign the request
	signature, date, err := SignRequest(
		creds.DevicePrivateKey,
		req.Method,
		path,
		body,
		creds.ADPToken,
	)
	if err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	// Add headers
	req.Header.Set("x-adp-token", creds.ADPToken)
	req.Header.Set("x-adp-alg", "SHA256withRSA:1.0")
	req.Header.Set("x-adp-signature", signature+":"+date)

	return nil
}

// DeregisterDevice deregisters the current device from Audible.
func (c *Client) DeregisterDevice(ctx context.Context) error {
	c.mu.RLock()
	creds := c.credentials
	c.mu.RUnlock()

	if creds == nil {
		return ErrNotAuthenticated
	}

	// Build deregistration request
	deregURL := fmt.Sprintf("https://api.%s/auth/deregister", c.marketplace.AmazonDomain())

	body := map[string]interface{}{
		"deregister_all_existing_accounts": false,
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", deregURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregistration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deregistration failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Clear credentials
	c.mu.Lock()
	c.credentials = nil
	c.mu.Unlock()

	return nil
}

// refreshToken refreshes the access token using the refresh token.
func (c *Client) doRefreshToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.credentials == nil {
		return ErrNotAuthenticated
	}

	refreshURL := fmt.Sprintf("https://api.%s/auth/token", c.marketplace.AmazonDomain())

	body := url.Values{
		"app_name":             {AppName},
		"app_version":          {AppVersion},
		"source_token":         {c.credentials.RefreshToken},
		"source_token_type":    {"refresh_token"},
		"requested_token_type": {"access_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, strings.NewReader(body.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	c.credentials.AccessToken = tokenResp.AccessToken
	c.credentials.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return nil
}
