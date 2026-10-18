package audible_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"

	audible "github.com/mstrhakr/go-audible"
)

func TestSignRequestAcceptsPEMAndAndroidDERKeys(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := x509.MarshalPKCS1PrivateKey(key)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	standardPEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: pkcs1}))
	for name, encoded := range map[string]string{
		"PKCS1 PEM":   standardPEM,
		"PKCS8 PEM":   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})),
		"PKCS1 DER":   base64.StdEncoding.EncodeToString(pkcs1),
		"PKCS8 DER":   base64.StdEncoding.EncodeToString(pkcs8),
		"escaped PEM": strings.ReplaceAll(standardPEM, "\n", `\n`),
	} {
		t.Run(name, func(t *testing.T) {
			signature, date, err := audible.SignRequest(encoded, "POST", "/1.0/content/synthetic/licenserequest", `{"quality":"High"}`, "synthetic-adp")
			if err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(signature)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte(fmt.Sprintf("POST\n/1.0/content/synthetic/licenserequest\n%s\n{\"quality\":\"High\"}\nsynthetic-adp", date)))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], raw); err != nil {
				t.Fatal("signature does not verify:", err)
			}
		})
	}
}

func TestSignRequestRejectsMalformedAndNonRSAKeys(t *testing.T) {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ec)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{"", "not a key", base64.StdEncoding.EncodeToString([]byte("not DER")), base64.StdEncoding.EncodeToString(der)} {
		if _, _, err := audible.SignRequest(encoded, "GET", "/test", "", "synthetic"); err == nil {
			t.Fatal("invalid or non-RSA key accepted")
		}
	}
}
