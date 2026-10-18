package audible

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// DeviceProfile selects the virtual device used for a new registration.
// Existing credentials retain the device type with which they were registered.
type DeviceProfile string

const (
	IPhone                 DeviceProfile = ""
	Android                DeviceProfile = "android"
	androidDeviceType                    = "A10KISP2GWF0E4"
	androidAppVersion                    = "2090263407"
	androidAppName                       = "com.audible.application"
	androidModel                         = "sdk_gphone64_x86_64"
	androidOS                            = `google\/raven\/raven:14\/UPB5.230623.003\/10615560:user\/release-keys`
	androidSoftwareVersion               = "130050002"
	androidUserAgent                     = "Mozilla/5.0 (Linux; Android 14; sdk_gphone64_x86_64 Build/UPB5.230623.003; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/113.0.5672.136 Mobile Safari/537.36"
)

func (p DeviceProfile) validate() error {
	if p != IPhone && p != Android {
		return fmt.Errorf("unknown device profile %q", p)
	}
	return nil
}

func (p DeviceProfile) deviceType() string {
	if p == Android {
		return androidDeviceType
	}
	return DeviceTypeID
}

func (p DeviceProfile) clientID(serial string) string {
	return hex.EncodeToString([]byte(serial + "#" + p.deviceType()))
}

func (p DeviceProfile) serial() (string, error) {
	if p != Android {
		return GenerateDeviceSerial()
	}
	serial := make([]byte, 10)
	if _, err := io.ReadFull(rand.Reader, serial); err != nil {
		return "", err
	}
	return hex.EncodeToString(serial), nil
}

func (c *Client) credentialProfile() DeviceProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.credentials != nil && c.credentials.DeviceInfo.DeviceType == androidDeviceType {
		return Android
	}
	return IPhone
}

func (c *Client) authEndpoint(p DeviceProfile) string {
	endpoint := c.APIEndpoint()
	if p == Android && endpoint == c.marketplace.APIEndpoint() {
		return "https://api." + c.marketplace.AmazonDomain()
	}
	return endpoint
}

func (p DeviceProfile) userAgent() string {
	if p == Android {
		return androidUserAgent
	}
	return fmt.Sprintf("Audible/%s (iOS %s; %s)", AppVersion, OSVersion, DeviceModel)
}

func (p DeviceProfile) downloadUserAgent() string {
	if p == Android {
		return "com.audible.playersdk.player/3.96.1 (Linux;Android 14) AndroidXMedia3/1.3.0"
	}
	return p.userAgent()
}

func (p DeviceProfile) registrationBody(body map[string]interface{}, serial string, marketplace Marketplace) error {
	if p != Android {
		return nil
	}
	decoded, err := hex.DecodeString(serial)
	if err != nil || len(decoded) != 10 {
		return fmt.Errorf("Android device serial must contain 20 hexadecimal characters")
	}
	body["requested_token_type"] = []string{"bearer", "mac_dms", "store_authentication_cookie", "website_cookies"}
	body["cookies"].(map[string]interface{})["domain"] = "www." + marketplace.AmazonDomain()
	body["registration_data"] = map[string]interface{}{
		"domain": "DeviceLegacy", "device_type": androidDeviceType, "device_serial": serial,
		"app_name": androidAppName, "app_version": androidAppVersion, "device_model": androidModel,
		"os_version": androidOS, "software_version": androidSoftwareVersion,
		"device_name": "%FIRST_NAME%%FIRST_NAME_POSSESSIVE_STRING%%DUPE_STRATEGY_1ST%Audible for Android",
	}
	body["auth_data"].(map[string]interface{})["use_global_authentication"] = "true"
	body["device_metadata"] = map[string]interface{}{"device_os_family": "android", "device_type": androidDeviceType, "device_serial": serial, "manufacturer": "Google", "model": androidModel, "os_version": "34", "product": androidModel}
	frc, err := androidFRC(serial)
	if err != nil {
		return fmt.Errorf("encode Android registration context: %w", err)
	}
	body["user_context_map"] = map[string]string{"frc": frc}
	return nil
}

// androidFRC encodes Amazon's registration context: gzip JSON, AES-CBC and
// an eight-byte HMAC over the IV and ciphertext. The serial derives both keys.
func androidFRC(serial string) (string, error) {
	_, offset := time.Now().Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	context := map[string]string{"ApplicationName": androidAppName, "ApplicationVersion": androidAppVersion, "DeviceOSVersion": androidOS, "DeviceName": `ranchu\/Google\/` + androidModel, "ScreenWidthPixels": "1080", "ScreenHeightPixels": "2400", "ThirdPartyDeviceId": serial, "FirstPartyDeviceId": serial, "DeviceLanguage": "en_US", "TimeZone": fmt.Sprintf("%s%02d:%02d", sign, offset/3600, offset%3600/60), "Carrier": "T-Mobile"}
	raw, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(raw); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	plaintext := compressed.Bytes()
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	plaintext = append(plaintext, bytes.Repeat([]byte{byte(padding)}, padding)...)
	iv := make([]byte, aes.BlockSize)
	if _, err = io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	key := pbkdf2.Key([]byte(serial), []byte("AES/CBC/PKCS7Padding"), 1000, 16, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
	mac := hmac.New(sha256.New, pbkdf2.Key([]byte(serial), []byte("HmacSHA256"), 1000, 16, sha1.New))
	mac.Write(iv)
	mac.Write(ciphertext)
	encoded := append([]byte{0}, mac.Sum(nil)[:8]...)
	encoded = append(encoded, iv...)
	encoded = append(encoded, ciphertext...)
	return base64.StdEncoding.EncodeToString(encoded), nil
}
