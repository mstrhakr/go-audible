package audible

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildClientID(t *testing.T) {
	serial := "ABC123456789"
	id := buildClientID(serial)
	if id == "" {
		t.Fatal("client ID should not be empty")
	}
	// client ID is hex encoding of serial + "#" + DeviceTypeID
	decoded, err := hex.DecodeString(id)
	if err != nil {
		t.Fatalf("invalid hex string: %v", err)
	}
	if string(decoded) != serial+"#"+DeviceTypeID {
		t.Fatalf("expected %q got %q", serial+"#"+DeviceTypeID, string(decoded))
	}
}

func TestParseExpiresIn(t *testing.T) {
	if got := parseExpiresIn(float64(123)); got != 123 {
		t.Fatalf("expected 123 got %d", got)
	}
	if got := parseExpiresIn("456"); got != 456 {
		t.Fatalf("expected 456 got %d", got)
	}
	if got := parseExpiresIn(nil); got != 3600 {
		t.Fatalf("expected default 3600 got %d", got)
	}
}

func TestIsAllDigits(t *testing.T) {
	if !isAllDigits("123456") {
		t.Fatal("expected digits")
	}
	if isAllDigits("123a56") {
		t.Fatal("expected non-digits to fail")
	}
}

func TestBookIdentifierValidation(t *testing.T) {
	var b Book
	// ASIN
	if err := json.Unmarshal([]byte(`{"asin":"B012345678"}`), &b); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if b.BestID() != "B012345678" {
		t.Fatalf("expected as ASIN got %s", b.BestID())
	}

	// ISBN10
	if err := json.Unmarshal([]byte(`{"asin":"1234567890"}`), &b); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if b.BestID() != "1234567890" {
		t.Fatalf("expected ISBN10 got %s", b.BestID())
	}

	// ISBN13
	if err := json.Unmarshal([]byte(`{"asin":"1234567890123"}`), &b); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if b.BestID() != "1234567890123" {
		t.Fatalf("expected ISBN13 got %s", b.BestID())
	}

	// Other ID
	if err := json.Unmarshal([]byte(`{"asin":"X-OTHER-123"}`), &b); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if b.BestID() != "X-OTHER-123" {
		t.Fatalf("expected OtherID got %s", b.BestID())
	}
}

func TestClientAPIEndpointOverride(t *testing.T) {
	c := NewClient(MarketplaceUS)
	if c.APIEndpoint() != MarketplaceUS.APIEndpoint() {
		t.Fatalf("expected default endpoint, got %s", c.APIEndpoint())
	}
	c.SetAPIEndpoint("http://test.local")
	if c.APIEndpoint() != "http://test.local" {
		t.Fatalf("override failed, got %s", c.APIEndpoint())
	}
}

func TestLoadSaveCredentials(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{
		ADPToken:    "test-token",
		CustomerID:  "cust",
		Marketplace: "us",
	})
	f := filepath.Join(t.TempDir(), "creds.json")
	if err := c.SaveCredentials(f); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}

	c2 := NewClient(MarketplaceUS)
	if err := c2.LoadCredentials(f); err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if !c2.IsAuthenticated() {
		t.Fatal("expected authenticated after load")
	}
}

func TestRefreshAccessTokenBehavior(t *testing.T) {
	c := NewClient(MarketplaceUS)
	err := c.RefreshAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error when no credentials")
	}

	c.SetCredentials(&Credentials{ADPToken: "x", ExpiresAt: time.Now().Add(10 * time.Minute)})
	if err := c.RefreshAccessToken(context.Background()); err != nil {
		t.Fatalf("unexpected error on valid token: %v", err)
	}
}

func generateTestPrivateKey(t *testing.T) string {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	return string(pemBytes)
}

func TestSignRequest(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "x-token", DevicePrivateKey: generateTestPrivateKey(t)})
	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.signRequest(req, ""); err != nil {
		t.Fatalf("signRequest failed: %v", err)
	}
	if req.Header.Get("x-adp-token") != "x-token" {
		t.Fatalf("x-adp-token header missing")
	}
	if req.Header.Get("x-adp-alg") != "SHA256withRSA:1.0" {
		t.Fatalf("unexpected x-adp-alg")
	}
	if req.Header.Get("x-adp-signature") == "" {
		t.Fatalf("missing signature")
	}
}

func TestDoRefreshTokenAndDeregisterDevice(t *testing.T) {
	ck := generateTestPrivateKey(t)
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "adptoken", DevicePrivateKey: ck, AccessToken: "old", RefreshToken: "refreshtoken", ExpiresAt: time.Now().Add(-time.Hour)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "newtoken", "expires_in": 3600, "token_type": "bearer"})
		case "/auth/deregister":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c.SetAPIEndpoint(server.URL)

	if err := c.doRefreshToken(context.Background()); err != nil {
		t.Fatalf("doRefreshToken failed: %v", err)
	}
	if c.GetCredentials().AccessToken != "newtoken" {
		t.Fatalf("expected new access token got %q", c.GetCredentials().AccessToken)
	}

	if err := c.DeregisterDevice(context.Background()); err != nil {
		t.Fatalf("DeregisterDevice failed: %v", err)
	}
	if c.IsAuthenticated() {
		t.Fatal("client should be logged out after deregister")
	}
}

func TestAuthenticateFromFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/register" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		data, err := os.ReadFile(filepath.Join("testdata", "auth_register_response.json"))
		if err != nil {
			w.Write([]byte(`{"error":"fixture missing"}`))
			return
		}
		w.Write(data)
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetAPIEndpoint(server.URL)
	c.SetMarketplace(MarketplaceUS)

	if err := c.Authenticate(context.Background(), DeviceRegistrationRequest{AuthorizationCode: "code", CodeVerifier: "verifier", DeviceSerial: "serial"}); err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated after Authenticate")
	}
}

func TestDoAPIRequest(t *testing.T) {
	key := generateTestPrivateKey(t)
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "x-token", DevicePrivateKey: key, AccessToken: "access1", RefreshToken: "refresh1", ExpiresAt: time.Now().Add(1 * time.Hour)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c.SetAPIEndpoint(server.URL)

	body, err := c.doAPIRequest(context.Background(), "GET", "/test", "")
	if err != nil {
		t.Fatalf("doAPIRequest failed: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", string(body))
	}
}
