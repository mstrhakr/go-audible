package audible

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestBuildInitCookies(t *testing.T) {
	cookies := buildInitCookies("audible.com")
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(cookies))
	}
	if cookies[0].Name != "frc" || cookies[0].Domain != ".audible.com" {
		t.Fatalf("unexpected frc cookie: %#v", cookies[0])
	}
	if cookies[1].Name != "map-md" || cookies[1].Domain != ".audible.com" {
		t.Fatalf("unexpected map-md cookie: %#v", cookies[1])
	}
	if cookies[2].Name != "amzn-app-id" || cookies[2].Domain != ".audible.com" {
		t.Fatalf("unexpected amzn-app-id cookie: %#v", cookies[2])
	}
	if _, err := base64.StdEncoding.DecodeString(cookies[0].Value); err != nil {
		t.Fatalf("frc not valid base64: %v", err)
	}
	if _, err := base64.StdEncoding.DecodeString(cookies[1].Value); err != nil {
		t.Fatalf("map-md not valid base64: %v", err)
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

func loadFixture(t *testing.T, name string) []byte {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to identify caller frame")
	}
	path := filepath.Join(filepath.Dir(sourceFile), "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		// fallback to relative path, e.g. when working outside module root
		path = filepath.Join("testdata", name)
		b, err = os.ReadFile(path)
	}
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}
	return b
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
	respJSON := string(loadFixture(t, "auth_register_response.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/register" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
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

func TestDoAPIRequestStatusMapping(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "token", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	c.SetAPIEndpoint(server.URL)

	_, err := c.doAPIRequest(context.Background(), "GET", "/test", "")
	if err != ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err = c.doAPIRequest(context.Background(), "GET", "/test", "")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err = c.doAPIRequest(context.Background(), "GET", "/test", "")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetAllLibrary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		if page == "1" {
			_, _ = w.Write([]byte(`{"items":[{"asin":"B001","title":"Test"}],"total_results":2}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[],"total_results":2}`))
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetAPIEndpoint(server.URL)
	c.SetCredentials(&Credentials{ADPToken: "token", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour)})

	books, err := c.GetAllLibrary(context.Background())
	if err != nil {
		t.Fatalf("GetAllLibrary failed: %v", err)
	}
	if len(books) != 1 || books[0].BestID() != "B001" {
		t.Fatalf("unexpected books %v", books)
	}
}

func TestExtractActivationBytes(t *testing.T) {
	activationData := make([]byte, 568)
	binary.LittleEndian.PutUint32(activationData[0:4], 0x04030201)
	blob := append([]byte("group_id"), activationData...)

	got, err := ExtractActivationBytes(blob)
	if err != nil {
		t.Fatalf("ExtractActivationBytes failed: %v", err)
	}
	if got != "04030201" {
		t.Errorf("expected 04030201 got %q", got)
	}
}

func TestExtractActivationBytesLegacy(t *testing.T) {
	raw := []byte("...license_response...00a4b6c8...")
	got, err := ExtractActivationBytesLegacy(raw)
	if err != nil {
		t.Fatalf("ExtractActivationBytesLegacy failed: %v", err)
	}
	if got != "00a4b6c8" {
		t.Errorf("expected 00a4b6c8 got %q", got)
	}
}

func TestFindHexPatternAndIsValidHex(t *testing.T) {
	if got := findHexPattern([]byte("xx00a4b6c8yy")); got != "00a4b6c8" {
		t.Errorf("findHexPattern = %q, want 00a4b6c8", got)
	}
	if !isValidHex("deadbeef") {
		t.Error("expected isValidHex(deadbeef)=true")
	}
	if isValidHex("not-hex") {
		t.Error("expected isValidHex(not-hex)=false")
	}
}

func TestCandidateCustomerIDs(t *testing.T) {
	out := candidateCustomerIDs("amzn1.account.ABC123", "User [XYZ456]")
	if len(out) != 3 {
		t.Fatalf("expected 3 customer IDs, got %v", out)
	}
	if out[0] != "amzn1.account.ABC123" || out[1] != "ABC123" || out[2] != "XYZ456" {
		t.Errorf("unexpected customer IDs: %v", out)
	}
}

func TestExtractUserIDFromVoucherMessage(t *testing.T) {
	if got := extractUserIDFromVoucherMessage("some text User [12345] more"); got != "12345" {
		t.Errorf("expected 12345 got %q", got)
	}
	if extractUserIDFromVoucherMessage("no user here") != "" {
		t.Error("expected empty user ID")
	}
}

func TestLooksLikeAAXCKeyIV(t *testing.T) {
	valid := strings.Repeat("a", 32)
	if !looksLikeAAXCKeyIV(valid, valid) {
		t.Error("expected looksLikeAAXCKeyIV true")
	}
	if looksLikeAAXCKeyIV("short", valid) {
		t.Error("expected false for wrong length")
	}
	if looksLikeAAXCKeyIV(strings.Repeat("g", 32), valid) {
		t.Error("expected false for non-hex")
	}
}

func TestFindKeyIV(t *testing.T) {
	key, iv := findKeyIV(map[string]any{"nested": map[string]any{"key": "1234567890abcdef1234567890abcdef", "iv": "abcdef1234567890abcdef1234567890"}})
	if key == "" || iv == "" {
		t.Fatal("expected key and iv")
	}
	key, iv = findKeyIV("{\"key\":\"11111111111111111111111111111111\",\"iv\":\"22222222222222222222222222222222\"}")
	if key != "11111111111111111111111111111111" || iv != "22222222222222222222222222222222" {
		t.Fatalf("unexpected key/iv %q/%q", key, iv)
	}
}

func TestFormatChaptersFile(t *testing.T) {
	chapters := []Chapter{{Title: "Start", StartOffsetMs: 3601000}, {Title: "End", StartOffsetMs: 0}}
	out := FormatChaptersFile(chapters)
	if !strings.Contains(out, "01:00:01.000 Start") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "00:00:00.000 End") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestNewClientWithHTTP(t *testing.T) {
	custom := &http.Client{Timeout: 1}
	c := NewClientWithHTTP(MarketplaceUS, custom)
	if c.APIEndpoint() != MarketplaceUS.APIEndpoint() {
		t.Fatalf("expected default endpoint got %s", c.APIEndpoint())
	}
	if c.httpClient != custom {
		t.Fatal("expected custom HTTP client")
	}
}

func TestMarketplaceAudibleDomain(t *testing.T) {
	if MarketplaceUK.AudibleDomain() != "audible.co.uk" {
		t.Fatalf("unexpected domain %s", MarketplaceUK.AudibleDomain())
	}
}

func TestGetActivationBytesCached(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ActivationBytes: "deadbeef"})
	resp, err := c.GetActivationBytes(context.Background())
	if err != nil {
		t.Fatalf("GetActivationBytes failed: %v", err)
	}
	if resp.ActivationBytes != "deadbeef" {
		t.Errorf("unexpected activation bytes %s", resp.ActivationBytes)
	}
}

func TestGetActivationBytesFetch(t *testing.T) {
	// Construct activation blob for server, with group_id and valid payload.
	activationData := make([]byte, 568)
	binary.LittleEndian.PutUint32(activationData[0:4], 0x01020304)
	blob := append([]byte("group_id"), activationData...)

	// Use a transport stub to avoid real network calls and return our crafted blob.
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(blob)),
			Header:     make(http.Header),
		}, nil
	})

	c := NewClient(MarketplaceUS)
	c.httpClient = &http.Client{Transport: transport}
	c.SetCredentials(&Credentials{ADPToken: "x", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour), DeviceInfo: DeviceInfo{DeviceSerialNumber: "serial", DeviceType: "type"}})

	res, err := c.GetActivationBytes(context.Background())
	if err != nil {
		t.Fatalf("GetActivationBytes fetch failed: %v", err)
	}
	if res.ActivationBytes != "01020304" {
		t.Errorf("unexpected activation bytes %s", res.ActivationBytes)
	}
}

func TestDecryptVoucher(t *testing.T) {
	deviceType, deviceSerial, customerID, asin := "type", "serial", "customer", "B001"
	plaintext := []byte(`{"key":"00112233445566778899aabbccddeeff","iv":"ffeeddccbbaa99887766554433221100"}`)
	padding := 16 - len(plaintext)%16
	for i := 0; i < padding; i++ {
		plaintext = append(plaintext, byte(padding))
	}
	buf := []byte(deviceType + deviceSerial + customerID + asin)
	digest := sha256.Sum256(buf)
	key := digest[0:16]
	iv := digest[16:32]
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}
	ciphertext := make([]byte, len(plaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plaintext)
	voucher := base64.StdEncoding.EncodeToString(ciphertext)

	gotKey, gotIV, err := DecryptVoucher(voucher, deviceType, deviceSerial, customerID, asin)
	if err != nil {
		t.Fatalf("DecryptVoucher failed: %v", err)
	}
	if gotKey != "00112233445566778899aabbccddeeff" || gotIV != "ffeeddccbbaa99887766554433221100" {
		t.Fatalf("DecryptVoucher returned wrong key/iv: %s/%s", gotKey, gotIV)
	}
}

func TestTrimNullBytes(t *testing.T) {
	if string(trimNullBytes([]byte("abc\x00\x00"))) != "abc" {
		t.Error("trimNullBytes did not trim null bytes")
	}
}

func TestGetChapters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content_metadata":{"chapter_info":{"runtime_length_ms":1234}}}`))
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetAPIEndpoint(server.URL)
	c.SetCredentials(&Credentials{ADPToken: "x", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour), DeviceInfo: DeviceInfo{DeviceSerialNumber: "serial", DeviceType: "type"}})

	chapters, err := c.GetChapters(context.Background(), "B001")
	if err != nil {
		t.Fatalf("GetChapters failed: %v", err)
	}
	if chapters.RuntimeLengthMs != 1234 {
		t.Fatalf("unexpected runtime length %d", chapters.RuntimeLengthMs)
	}
}

func TestGetLibraryAndGetBook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/1.0/library/") {
			w.Write([]byte(`{"item":{"asin":"B001","title":"Test"}}`))
			return
		}
		w.Write([]byte(`{"items":[{"asin":"B001","title":"Test"}],"total_results":1}`))
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetAPIEndpoint(server.URL)
	c.SetCredentials(&Credentials{ADPToken: "x", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour), DeviceInfo: DeviceInfo{DeviceSerialNumber: "serial", DeviceType: "type"}})

	lib, err := c.GetLibrary(context.Background())
	if err != nil {
		t.Fatalf("GetLibrary failed: %v", err)
	}
	if len(lib.Items) != 1 || lib.Items[0].BestID() != "B001" {
		t.Fatalf("unexpected lib item %v", lib.Items)
	}

	book, err := c.GetBook(context.Background(), "B001")
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if book.BestID() != "B001" {
		t.Fatalf("unexpected book id %s", book.BestID())
	}
}

func TestLibraryOptions(t *testing.T) {
	opt := &libraryOptions{}
	WithPageSize(10)(opt)
	WithPage(2)(opt)
	WithSortBy("-PurchaseDate")(opt)
	WithPurchasedAfter("2025-01-01")(opt)
	if opt.pageSize != 10 || opt.page != 2 || opt.sortBy != "-PurchaseDate" || opt.purchasedAfter != "2025-01-01" {
		t.Fatal("library options not set correctly")
	}
}

func TestBookBestID(t *testing.T) {
	cases := []struct {
		book Book
		want string
	}{
		{Book{ASIN: "B000"}, "B000"},
		{Book{ISBN10: "1234567890"}, "1234567890"},
		{Book{ISBN13: "1234567890123"}, "1234567890123"},
		{Book{OtherID: "OTHER"}, "OTHER"},
	}
	for _, tc := range cases {
		if got := tc.book.BestID(); got != tc.want {
			t.Errorf("BestID = %q, want %q", got, tc.want)
		}
	}
}

func TestBookDownloadable(t *testing.T) {
	tests := []struct {
		book Book
		want bool
	}{
		{Book{ContentType: "audiobook"}, true},
		{Book{ContentType: "ebook"}, false},
		{Book{FormatType: "AAX"}, true},
		{Book{ContentDeliveryType: "download"}, true},
		{Book{ContentType: ""}, false},
	}
	for _, tt := range tests {
		if got := tt.book.Downloadable(); got != tt.want {
			t.Errorf("Downloadable(%+v) = %v, want %v", tt.book, got, tt.want)
		}
	}
}

func TestBookUnmarshalJSON(t *testing.T) {
	var b Book
	if err := json.Unmarshal([]byte(`{"asin":"B012345678","title":"Test"}`), &b); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if b.BestID() != "B012345678" {
		t.Fatalf("unexpected BestID %q", b.BestID())
	}
	if b.Title != "Test" {
		t.Fatalf("unexpected title %q", b.Title)
	}
}

func TestCanDownloadNotDownloadable(t *testing.T) {
	c := NewClient(MarketplaceUS)
	ok, err := c.CanDownload(context.Background(), Book{ContentType: "ebook"})
	if err != nil || ok {
		t.Fatalf("expected false,nil got %v,%v", ok, err)
	}
}

func TestCdnTextErrorAndRetryable(t *testing.T) {
	err := &cdnTextError{peekBytes: 42, message: "File Assembly error: Invalid Audio Format."}
	if !strings.Contains(err.Error(), "42 bytes") {
		t.Errorf("unexpected error message: %q", err.Error())
	}
	if !isRetryableCDNError(err) {
		t.Error("expected retryable CDN error")
	}
	if isRetryableCDNError(&cdnTextError{peekBytes: 10, message: "Access denied"}) {
		t.Error("expected non-retryable CDN error")
	}
}

func TestAsStringAndParseLicenseDenialReasons(t *testing.T) {
	if asString(123) != "" {
		t.Error("expected empty on non-string")
	}
	if asString("foo") != "foo" {
		t.Error("expected foo")
	}

	reasons := parseLicenseDenialReasons([]any{map[string]any{"message": "m", "rejectionReason": "r", "validationType": "v"}})
	if len(reasons) != 1 || reasons[0].Message != "m" {
		t.Error("unexpected reasons parsed")
	}
	if parseLicenseDenialReasons(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestGetDownloadInfoDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content_license":{"asin":"B001","status_code":"Denied","message":"not licensed","license_denial_reasons":[{"message":"not eligible","rejectionReason":"RequesterEligibility","validationType":"Membership"}]}}`))
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetAPIEndpoint(server.URL)
	c.SetCredentials(&Credentials{ADPToken: "token", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "invalid", RefreshToken: "ref", ExpiresAt: time.Now().Add(1 * time.Hour), DeviceInfo: DeviceInfo{DeviceSerialNumber: "serial", DeviceType: "type"}})

	_, err := c.GetDownloadInfo(context.Background(), "B001")
	if err == nil {
		t.Fatal("expected LicenseDeniedError")
	}
	if _, ok := err.(*LicenseDeniedError); !ok {
		t.Fatalf("expected LicenseDeniedError got %T %v", err, err)
	}
}

func TestDownloadBookAttemptSuccess(t *testing.T) {
	streamData := strings.Repeat("a", cdnPeekBytes+100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", ""+strings.TrimSpace(""))
		_, _ = w.Write([]byte(streamData))
	}))
	defer server.Close()

	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "token", AccessToken: "access", DevicePrivateKey: generateTestPrivateKey(t), ExpiresAt: time.Now().Add(1 * time.Hour)})
	c.httpClient = &http.Client{}

	writer := &testDownloadWriter{}
	info := &DownloadInfo{ContentURL: server.URL}
	w, err := c.downloadBookAttempt(context.Background(), "B001", writer, info)
	if err != nil {
		t.Fatalf("downloadBookAttempt failed: %v", err)
	}
	if w != int64(len(streamData)) {
		t.Fatalf("expected %d bytes, got %d", len(streamData), w)
	}
	if !writer.completed {
		t.Fatal("expected writer to be complete")
	}
}

func TestExtractActivationBytesErrors(t *testing.T) {
	// no group_id marker
	_, err := ExtractActivationBytes([]byte("abcd"))
	if err != ErrInvalidActivation {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}

	// too short blob with group_id
	_, err = ExtractActivationBytes([]byte("group_id"))
	if err == nil {
		t.Fatal("expected error for short blob")
	}

	// server error marker
	_, err = ExtractActivationBytes([]byte("group_id" + strings.Repeat("x", 568) + "BAD_LOGIN"))
	if err == nil || !strings.Contains(err.Error(), "activation request rejected") {
		t.Fatalf("unexpected error for BAD_LOGIN: %v", err)
	}
}

func TestExtractActivationBytesLegacyHeuristic(t *testing.T) {
	// no marker
	_, err := ExtractActivationBytesLegacy([]byte("no marker"))
	if err != ErrInvalidActivation {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}

	// marker with hex pattern
	blob := []byte("prefix license_response 00a4b6c8 suffix")
	got, err := ExtractActivationBytesLegacy(blob)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "00a4b6c8" {
		t.Fatalf("expected 00a4b6c8, got %s", got)
	}
}

func TestHexHelpers(t *testing.T) {
	if !isValidHex("deadbeef") {
		t.Fatal("expected valid hex")
	}
	if isValidHex("nothex") {
		t.Fatal("expected invalid hex")
	}
	if min(5, 3) != 3 || min(1, 2) != 1 {
		t.Fatal("min function failed")
	}
}

func TestDecryptVoucherInvalid(t *testing.T) {
	_, _, err := DecryptVoucher("not-base64", "type", "serial", "cust", "asin")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	// Create a voucher with missing key/iv in JSON.
	plaintext := []byte("{}")
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	for i := 0; i < padding; i++ {
		plaintext = append(plaintext, byte(padding))
	}
	buf := []byte("type" + "serial" + "cust" + "asin")
	digest := sha256.Sum256(buf)
	key := digest[0:16]
	iv := digest[16:32]
	block, _ := aes.NewCipher(key)
	ciphertext := make([]byte, len(plaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plaintext)
	voucher := base64.StdEncoding.EncodeToString(ciphertext)

	_, _, err = DecryptVoucher(voucher, "type", "serial", "cust", "asin")
	if err == nil {
		t.Fatal("expected error for missing key/iv")
	}
}

func TestCanDownloadErrorPaths(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "x", DevicePrivateKey: generateTestPrivateKey(t), AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(1 * time.Hour)})
	c.httpClient = &http.Client{}

	ok, err := c.CanDownload(context.Background(), Book{})
	if err != nil || ok {
		t.Fatalf("expected false,nil for empty book, got %v,%v", ok, err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/1.0/content/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"content_license":{"asin":"B001","status_code":"Ok","content_url":"` + server.URL + `/download"}}`))
			return
		}
		if r.URL.Path == "/download" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c.SetAPIEndpoint(server.URL)
	_, err = c.CanDownload(context.Background(), Book{ASIN: "B001", ContentType: "audiobook"})
	if err == nil || !strings.Contains(err.Error(), "download URL probe returned status") {
		t.Fatalf("unexpected error for bad probe status: %v", err)
	}
}

func TestAudibleClientCoreMethods(t *testing.T) {
	c := NewClient(MarketplaceUS)
	if c.IsAuthenticated() {
		t.Fatal("expected unauthenticated client")
	}
	if c.APIEndpoint() != MarketplaceUS.APIEndpoint() {
		t.Fatalf("unexpected API endpoint %s", c.APIEndpoint())
	}

	c.SetAPIEndpoint("https://localhost")
	if c.APIEndpoint() != "https://localhost" {
		t.Fatalf("unexpected overridden API endpoint %s", c.APIEndpoint())
	}

	c.SetMarketplace(MarketplaceUK)
	if c.Marketplace().CountryCode != "uk" {
		t.Fatalf("unexpected marketplace %s", c.Marketplace().CountryCode)
	}

	creds := &Credentials{ADPToken: "test", DevicePrivateKey: generateTestPrivateKey(t), CustomerID: "cust", Marketplace: "us"}
	c.SetCredentials(creds)
	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated after set credentials")
	}

	data, err := c.MarshalCredentials()
	if err != nil {
		t.Fatalf("MarshalCredentials failed: %v", err)
	}

	c2 := NewClient(MarketplaceUS)
	if err := c2.UnmarshalCredentials(data); err != nil {
		t.Fatalf("UnmarshalCredentials failed: %v", err)
	}
	if !c2.IsAuthenticated() {
		t.Fatal("expected c2 authenticated after unmarshal")
	}

	f := filepath.Join(t.TempDir(), "creds.json")
	if err := c.SaveCredentials(f); err != nil {
		t.Fatalf("SaveCredentials failed: %v", err)
	}
	c3 := NewClient(MarketplaceUS)
	if err := c3.LoadCredentials(f); err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if c3.GetCredentials().ADPToken != "test" {
		t.Fatalf("LoadCredentials value mismatch")
	}
}

func TestAuthUtilities(t *testing.T) {
	cookies := buildInitCookies("audible.com")
	if len(cookies) != 3 || cookies[0].Name != "frc" || cookies[1].Name != "map-md" || cookies[2].Name != "amzn-app-id" {
		t.Fatal("buildInitCookies returned unexpected cookies")
	}

	id := buildClientID("ABC123")
	if id == "" {
		t.Fatal("buildClientID returned empty")
	}
}

func TestCryptoUtilities(t *testing.T) {
	serial, err := GenerateDeviceSerial()
	if err != nil || serial == "" {
		t.Fatalf("GenerateDeviceSerial failed: %v", err)
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil || verifier == "" {
		t.Fatalf("GenerateCodeVerifier failed: %v", err)
	}
	challenge := GenerateCodeChallenge(verifier)
	if challenge == "" {
		t.Fatal("GenerateCodeChallenge returned empty")
	}

	state, err := GenerateRandomState()
	if err != nil || state == "" {
		t.Fatalf("GenerateRandomState failed: %v", err)
	}

	// Sign request invalid key
	_, _, err = SignRequest("badkey", "GET", "/", "", "token")
	if err == nil {
		t.Fatal("SignRequest should fail with invalid key")
	}

	// Encrypt/Decrypt AES
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	plaintext := []byte("hello world")
	ciphertext, err := EncryptAES(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptAES failed: %v", err)
	}
	decoded, err := DecryptAES(ciphertext, key)
	if err != nil {
		t.Fatalf("DecryptAES failed: %v", err)
	}
	if string(decoded) != string(plaintext) {
		t.Fatalf("DecryptAES got %q, want %q", decoded, plaintext)
	}

	// Invalid AES key length
	_, err = EncryptAES(plaintext, []byte("short"))
	if err == nil {
		t.Fatal("EncryptAES should fail with invalid key length")
	}

	_, err = DecryptAES([]byte("abc"), key)
	if err == nil {
		t.Fatal("DecryptAES should fail for short ciphertext")
	}
}

func TestXXTEAUtilities(t *testing.T) {
	key := []byte("1234567890abcdef")
	data := []byte("abcd1234abcd1234") // 16 bytes multiple of 4
	enc, err := XXTEAEncrypt(data, key)
	if err != nil {
		t.Fatalf("XXTEAEncrypt failed: %v", err)
	}
	dec, err := XXTEADecrypt(enc, key)
	if err != nil {
		t.Fatalf("XXTEADecrypt failed: %v", err)
	}
	if string(dec) != string(data) {
		t.Fatalf("XXTEA roundtrip mismatch")
	}
}

func TestDoDownloadRequestErrors(t *testing.T) {
	c := NewClient(MarketplaceUS)
	_, err := c.doDownloadRequest(context.Background(), "http://example.com")
	if err != ErrNotAuthenticated {
		t.Fatalf("expected ErrNotAuthenticated, got %v", err)
	}

	c.SetCredentials(&Credentials{ADPToken: "token", AccessToken: "access", DevicePrivateKey: generateTestPrivateKey(t), ExpiresAt: time.Now().Add(1 * time.Hour)})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	c.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}

	resp, err := c.doDownloadRequest(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("doDownloadRequest failed: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "ok" {
		t.Fatalf("unexpected body %s", string(b))
	}
}

func TestDownloadHelpers(t *testing.T) {
	if !isHex("deadBEEF") {
		t.Fatal("expected isHex true")
	}
	if isHex("not_hex") {
		t.Fatal("expected isHex false")
	}
	if !looksLikeAAXCKeyIV("0123456789abcdef0123456789abcdef", "0123456789abcdef0123456789abcdef") {
		t.Fatal("expected looksLikeAAXCKeyIV true")
	}
	if looksLikeAAXCKeyIV("short", "short") {
		t.Fatal("expected looksLikeAAXCKeyIV false")
	}

	key, iv := findKeyIV(map[string]any{"nested": map[string]any{"key": "11111111111111111111111111111111", "iv": "22222222222222222222222222222222"}})
	if key != "11111111111111111111111111111111" || iv != "22222222222222222222222222222222" {
		t.Fatalf("unexpected keyiv %s %s", key, iv)
	}

	key, iv = findKeyIV(`{"key":"33333333333333333333333333333333","iv":"44444444444444444444444444444444"}`)
	if key != "33333333333333333333333333333333" || iv != "44444444444444444444444444444444" {
		t.Fatalf("unexpected keyiv from raw json %s %s", key, iv)
	}

	if extractUserIDFromVoucherMessage("Welcome User [98765]!") != "98765" {
		t.Fatal("extractUserIDFromVoucherMessage failed")
	}

	ids := candidateCustomerIDs("amzn1.account.XYZ", "User [ABC]")
	if len(ids) != 3 {
		t.Fatalf("candidateCustomerIDs expected3 got %d", len(ids))
	}
}

func TestDownloadBookFlow(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "token", AccessToken: "access", DevicePrivateKey: generateTestPrivateKey(t), ExpiresAt: time.Now().Add(1 * time.Hour), DeviceInfo: DeviceInfo{DeviceSerialNumber: "serial", DeviceType: "type"}, CustomerID: "amzn1.account.xyz"})
	c.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/1.0/content/") {
			body := `{"content_license":{"asin":"B001","status_code":"Ok","content_url":"https://cdn.local/download"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if strings.Contains(req.URL.Path, "/download") {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", cdnPeekBytes+10))), Header: make(http.Header)}, nil
		}
		return nil, nil
	})}

	writer := &testDownloadWriter{}
	w, err := c.DownloadBook(context.Background(), "B001", writer)
	if err != nil {
		t.Fatalf("DownloadBook failed: %v", err)
	}
	if w != int64(cdnPeekBytes+10) {
		t.Fatalf("DownloadBook bytes=%d expected %d", w, cdnPeekBytes+10)
	}
	if !writer.completed {
		t.Fatal("expected writer completed")
	}
}

func TestDownloadBookInfoMissingURL(t *testing.T) {
	c := NewClient(MarketplaceUS)
	c.SetCredentials(&Credentials{ADPToken: "token", AccessToken: "access", DevicePrivateKey: generateTestPrivateKey(t), ExpiresAt: time.Now().Add(1 * time.Hour)})
	c.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/1.0/content/") {
			body := `{"content_license":{"status_code":"Ok"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return nil, nil
	})}
	_, err := c.DownloadBook(context.Background(), "B001", &testDownloadWriter{})
	if err == nil {
		t.Fatal("expected error from download attempt")
	}
	if !strings.Contains(err.Error(), "download request failed") && !strings.Contains(err.Error(), "no content URL available") {
		t.Fatalf("unexpected error from DownloadBook: %v", err)
	}
}

type testDownloadWriter struct {
	data      []byte
	completed bool
}

func (w *testDownloadWriter) OnStart(asin string, contentLength int64, info *DownloadInfo) error {
	return nil
}

func (w *testDownloadWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}

func (w *testDownloadWriter) OnProgress(bytesWritten, totalBytes int64) error {
	return nil
}

func (w *testDownloadWriter) OnComplete() error {
	w.completed = true
	return nil
}
