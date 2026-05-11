package audible

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthenticateEndToEndWithMockServer(t *testing.T) {
	type capturedRequest struct {
		AuthData struct {
			AuthorizationCode string `json:"authorization_code"`
			CodeVerifier      string `json:"code_verifier"`
			ClientID          string `json:"client_id"`
		} `json:"auth_data"`
		RegistrationData struct {
			DeviceSerial string `json:"device_serial"`
			DeviceType   string `json:"device_type"`
		} `json:"registration_data"`
	}

	var gotReq capturedRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/auth/register" {
			t.Fatalf("path = %s, want /auth/register", r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll body error: %v", err)
		}
		if err := json.Unmarshal(body, &gotReq); err != nil {
			t.Fatalf("unmarshal request error: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"response": {
				"success": {
					"extensions": {
						"device_info": {
							"device_name": "Audible for iPhone",
							"device_serial_number": "SERIAL-123",
							"device_type": "A2CZJZGLK2JJVM"
						},
						"customer_info": {
							"user_id": "cust-42",
							"name": "Tester"
						}
					},
					"tokens": {
						"bearer": {
							"access_token": "access-token",
							"refresh_token": "refresh-token",
							"expires_in": "120"
						},
						"mac_dms": {
							"device_private_key": "private-key",
							"adp_token": "adp-token"
						},
						"website_cookies": [
							{"Name": "session-id", "Value": "abc"}
						]
					}
				}
			},
			"request_id": "req-1"
		}`))
	}))
	defer ts.Close()

	client := NewClient(MarketplaceUS)
	client.SetAPIEndpoint(ts.URL)

	req := DeviceRegistrationRequest{
		AuthorizationCode: "auth-code-xyz",
		CodeVerifier:      "verifier-xyz",
		DeviceSerial:      "serial-xyz",
	}

	start := time.Now()
	if err := client.Authenticate(context.Background(), req); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if gotReq.AuthData.AuthorizationCode != "auth-code-xyz" {
		t.Fatalf("request auth_data.authorization_code = %q", gotReq.AuthData.AuthorizationCode)
	}
	if gotReq.AuthData.CodeVerifier != "verifier-xyz" {
		t.Fatalf("request auth_data.code_verifier = %q", gotReq.AuthData.CodeVerifier)
	}
	if gotReq.RegistrationData.DeviceSerial != "serial-xyz" {
		t.Fatalf("request registration_data.device_serial = %q", gotReq.RegistrationData.DeviceSerial)
	}
	if gotReq.RegistrationData.DeviceType != DeviceTypeID {
		t.Fatalf("request registration_data.device_type = %q, want %q", gotReq.RegistrationData.DeviceType, DeviceTypeID)
	}
	if !strings.HasPrefix(gotReq.AuthData.ClientID, "73657269616c2d78797a") {
		t.Fatalf("request auth_data.client_id looks wrong: %q", gotReq.AuthData.ClientID)
	}

	creds := client.GetCredentials()
	if creds == nil {
		t.Fatalf("credentials are nil after Authenticate")
	}
	if creds.ADPToken != "adp-token" || creds.AccessToken != "access-token" || creds.RefreshToken != "refresh-token" {
		t.Fatalf("credential tokens not set correctly: %+v", creds)
	}
	if creds.DevicePrivateKey != "private-key" {
		t.Fatalf("DevicePrivateKey = %q, want private-key", creds.DevicePrivateKey)
	}
	if creds.CustomerID != "cust-42" {
		t.Fatalf("CustomerID = %q, want cust-42", creds.CustomerID)
	}
	if creds.Marketplace != "us" {
		t.Fatalf("Marketplace = %q, want us", creds.Marketplace)
	}
	if !creds.ExpiresAt.After(start.Add(100 * time.Second)) {
		t.Fatalf("ExpiresAt too early: %v", creds.ExpiresAt)
	}
}
