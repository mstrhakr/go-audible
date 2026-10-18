package audible

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAndroidSignInUsesMatchingDeviceAndMarketplace(t *testing.T) {
	for _, market := range []Marketplace{MarketplaceUS, MarketplaceUK, MarketplaceDE} {
		t.Run(market.CountryCode, func(t *testing.T) {
			client := NewClient(market)
			auth, err := client.GetAuthURLForDevice(Android)
			if err != nil {
				t.Fatal(err)
			}
			serial, err := hex.DecodeString(auth.DeviceSerial)
			if err != nil || len(serial) != 10 {
				t.Fatalf("Android serial is not ten bytes: %q", auth.DeviceSerial)
			}
			u, _ := url.Parse(auth.URL)
			q := u.Query()
			id, err := hex.DecodeString(strings.TrimPrefix(q.Get("openid.oa2.client_id"), "device:"))
			if err != nil {
				t.Fatal(err)
			}
			if string(id) != auth.DeviceSerial+"#A10KISP2GWF0E4" {
				t.Fatalf("client id does not match Android serial/type: %s", id)
			}
			for key, want := range map[string]string{"openid.return_to": "https://www." + market.AudibleDomain() + "/ap/maplanding", "openid.assoc_handle": "amzn_audible_android_aui_" + market.CountryCode, "pageId": "amzn_audible_android_aui_v2_dark_" + market.CountryCode, "disableLoginPrepopulate": "1", "openid.oa2.code_challenge": GenerateCodeChallenge(auth.CodeVerifier)} {
				if q.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, q.Get(key), want)
				}
			}
			if q.Has("forceMobileLayout") {
				t.Error("Android login contains iPhone layout parameter")
			}
			if got := client.authEndpoint(Android); got != "https://api."+market.AmazonDomain() {
				t.Errorf("registration endpoint = %s", got)
			}
		})
	}
}

func TestAndroidRegistrationAndReloadKeepTheDeviceProfile(t *testing.T) {
	var registered bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/register":
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			reg := body["registration_data"].(map[string]interface{})
			auth := body["auth_data"].(map[string]interface{})
			metadata := body["device_metadata"].(map[string]interface{})
			if reg["device_type"] != "A10KISP2GWF0E4" || reg["domain"] != "DeviceLegacy" || reg["app_name"] != "com.audible.application" {
				t.Errorf("incorrect Android registration: %v", reg)
			}
			if auth["use_global_authentication"] != "true" || metadata["device_serial"] != reg["device_serial"] || metadata["device_type"] != reg["device_type"] {
				t.Error("registration identity differs between request sections")
			}
			if body["user_context_map"].(map[string]interface{})["frc"] == "" {
				t.Error("missing registration context")
			}
			if !strings.Contains(r.UserAgent(), "Android") {
				t.Errorf("registration user agent: %s", r.UserAgent())
			}
			registered = true
			// Omitting device_info checks that the request identity survives persistence.
			w.Write([]byte(`{"response":{"success":{"tokens":{"bearer":{"access_token":"access","refresh_token":"refresh","expires_in":3600},"mac_dms":{"adp_token":"adp","device_private_key":"unused"}},"extensions":{"customer_info":{"user_id":"synthetic"}}}}}`))
		case "/auth/token":
			r.ParseForm()
			if r.Form.Get("app_name") != "com.audible.application" || r.Form.Get("app_version") != "2090263407" {
				t.Error("refresh reverted to iPhone application")
			}
			w.Write([]byte(`{"access_token":"new-access","expires_in":3600}`))
		case "/audio":
			if !strings.Contains(r.UserAgent(), "Android") {
				t.Error("download reverted to iPhone headers")
			}
			w.Write([]byte("synthetic audio"))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(MarketplaceUS)
	client.SetAPIEndpoint(server.URL)
	auth, err := client.GetAuthURLForDevice(Android)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Authenticate(context.Background(), DeviceRegistrationRequest{Profile: Android, DeviceSerial: auth.DeviceSerial, CodeVerifier: auth.CodeVerifier, AuthorizationCode: "synthetic-code"}); err != nil {
		t.Fatal(err)
	}
	if !registered {
		t.Fatal("registration not sent")
	}
	data, err := json.Marshal(client.GetCredentials())
	if err != nil {
		t.Fatal(err)
	}
	var saved Credentials
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	saved.ExpiresAt = time.Now().Add(-time.Hour)
	reloaded := NewClient(MarketplaceUS)
	reloaded.SetAPIEndpoint(server.URL)
	reloaded.SetCredentials(&saved)
	if reloaded.credentialProfile() != Android {
		t.Fatal("persisted credentials lost Android profile")
	}
	if err := reloaded.doRefreshToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	response, err := reloaded.doDownloadRequest(context.Background(), server.URL+"/audio")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestIPhoneRemainsTheDefaultAndRejectsUnknownProfiles(t *testing.T) {
	client := NewClient(MarketplaceUS)
	auth, err := client.GetAuthURL()
	if err != nil {
		t.Fatal(err)
	}
	serial, err := hex.DecodeString(auth.DeviceSerial)
	if err != nil || len(serial) != 16 {
		t.Fatal("default iPhone serial changed")
	}
	u, _ := url.Parse(auth.URL)
	if u.Query().Get("openid.assoc_handle") != "amzn_audible_ios_us" {
		t.Fatal("default login profile changed")
	}
	if _, err := client.GetAuthURLForDevice("invalid"); err == nil {
		t.Error("unknown profile accepted")
	}
	if err := client.Authenticate(context.Background(), DeviceRegistrationRequest{Profile: Android, DeviceSerial: strings.Repeat("a", 40), CodeVerifier: "test"}); err == nil {
		t.Error("old twenty-byte Android serial accepted")
	}
}
