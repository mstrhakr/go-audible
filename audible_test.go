package audible_test

import (
	"testing"

	"github.com/mstrhakr/go-audible"
)

func TestNewClient(t *testing.T) {
	client := audible.NewClient(audible.MarketplaceUS)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.IsAuthenticated() {
		t.Error("New client should not be authenticated")
	}
}

func TestGetMarketplace(t *testing.T) {
	tests := []struct {
		code   string
		want   string
		wantOK bool
	}{
		{"us", "United States", true},
		{"uk", "United Kingdom", true},
		{"de", "Germany", true},
		{"fr", "France", true},
		{"invalid", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			m, ok := audible.GetMarketplace(tt.code)
			if ok != tt.wantOK {
				t.Errorf("GetMarketplace(%q) ok = %v, want %v", tt.code, ok, tt.wantOK)
			}
			if ok && m.Name != tt.want {
				t.Errorf("GetMarketplace(%q).Name = %q, want %q", tt.code, m.Name, tt.want)
			}
		})
	}
}

func TestAllMarketplaces(t *testing.T) {
	markets := audible.AllMarketplaces()

	if len(markets) < 5 {
		t.Errorf("Expected at least 5 marketplaces, got %d", len(markets))
	}

	// Check US is in the list
	found := false
	for _, m := range markets {
		if m.CountryCode == "us" {
			found = true
			break
		}
	}
	if !found {
		t.Error("US marketplace not found in AllMarketplaces")
	}
}

func TestMarketplaceUS(t *testing.T) {
	m := audible.MarketplaceUS

	if m.Name != "United States" {
		t.Errorf("MarketplaceUS.Name = %q, want %q", m.Name, "United States")
	}
	if m.CountryCode != "us" {
		t.Errorf("MarketplaceUS.CountryCode = %q, want %q", m.CountryCode, "us")
	}
	if m.Domain != "com" {
		t.Errorf("MarketplaceUS.Domain = %q, want %q", m.Domain, "com")
	}
	if m.AmazonDomain() != "amazon.com" {
		t.Errorf("MarketplaceUS.AmazonDomain() = %q, want %q", m.AmazonDomain(), "amazon.com")
	}
	if m.APIEndpoint() != "https://api.audible.com" {
		t.Errorf("MarketplaceUS.APIEndpoint() = %q, want %q", m.APIEndpoint(), "https://api.audible.com")
	}
}

func TestClientCredentials(t *testing.T) {
	client := audible.NewClient(audible.MarketplaceUS)

	// Initially no credentials
	if creds := client.GetCredentials(); creds != nil {
		t.Error("New client should have nil credentials")
	}

	// Set credentials
	creds := &audible.Credentials{
		ADPToken:    "test-token",
		CustomerID:  "test-customer",
		Marketplace: "us",
	}
	client.SetCredentials(creds)

	// Should now be authenticated
	if !client.IsAuthenticated() {
		t.Error("Client should be authenticated after setting credentials")
	}

	// Get credentials should return a copy
	retrieved := client.GetCredentials()
	if retrieved == nil {
		t.Fatal("GetCredentials returned nil")
	}
	if retrieved.ADPToken != "test-token" {
		t.Errorf("GetCredentials().ADPToken = %q, want %q", retrieved.ADPToken, "test-token")
	}
}

func TestClientMarshalUnmarshalCredentials(t *testing.T) {
	client := audible.NewClient(audible.MarketplaceUS)

	creds := &audible.Credentials{
		ADPToken:     "test-adp-token",
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		CustomerID:   "test-customer-id",
		Marketplace:  "us",
	}
	client.SetCredentials(creds)

	// Marshal
	data, err := client.MarshalCredentials()
	if err != nil {
		t.Fatalf("MarshalCredentials failed: %v", err)
	}

	// Create new client and unmarshal
	client2 := audible.NewClient(audible.MarketplaceUS)
	if err := client2.UnmarshalCredentials(data); err != nil {
		t.Fatalf("UnmarshalCredentials failed: %v", err)
	}

	// Verify
	retrieved := client2.GetCredentials()
	if retrieved.ADPToken != "test-adp-token" {
		t.Errorf("UnmarshalCredentials: ADPToken = %q, want %q", retrieved.ADPToken, "test-adp-token")
	}
	if retrieved.CustomerID != "test-customer-id" {
		t.Errorf("UnmarshalCredentials: CustomerID = %q, want %q", retrieved.CustomerID, "test-customer-id")
	}
}

func TestHandleAuthRedirect(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "valid redirect",
			url:  "https://www.amazon.com/ap/maplanding?openid.oa2.authorization_code=ANdNAVhyhqirUelHGEHA&openid.return_to=foo",
			want: "ANdNAVhyhqirUelHGEHA",
		},
		{
			name:    "missing code",
			url:     "https://www.amazon.com/ap/maplanding?openid.return_to=foo",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			url:     "://bad",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := audible.HandleAuthRedirect(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if code != tt.want {
				t.Errorf("HandleAuthRedirect() = %q, want %q", code, tt.want)
			}
		})
	}
}

func TestGetAuthURL(t *testing.T) {
	client := audible.NewClient(audible.MarketplaceUS)

	// Default (no callback) — should use maplanding
	authURL, err := client.GetAuthURL()
	if err != nil {
		t.Fatalf("GetAuthURL failed: %v", err)
	}

	if authURL.URL == "" {
		t.Error("AuthURL.URL is empty")
	}
	if authURL.CodeVerifier == "" {
		t.Error("AuthURL.CodeVerifier is empty")
	}
	if authURL.DeviceSerial == "" {
		t.Error("AuthURL.DeviceSerial is empty")
	}

	// Verify URL contains expected parameters (maplanding return_to)
	for _, param := range []string{
		"openid.oa2.response_type=code",
		"openid.oa2.code_challenge_method=S256",
		"pageId=amzn_audible_ios",
		"forceMobileLayout=true",
		"marketPlaceId=AF2M0KC94RCEA",
		"openid.assoc_handle=amzn_audible_ios_us",
		"amazon.com%2Fap%2Fmaplanding",
	} {
		if !contains(authURL.URL, param) {
			t.Errorf("AuthURL.URL missing expected param %q", param)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
