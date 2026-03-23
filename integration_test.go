//go:build integration
// +build integration

package audible

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadIntegrationCredentials(t *testing.T) *Credentials {
	t.Helper()

	// local fixture (in repo)
	for _, candidate := range []string{
		"testdata/credentials.json",
		filepath.Join("..", "testdata", "credentials.json"),
	} {
		if data, err := os.ReadFile(candidate); err == nil {
			var creds Credentials
			if err := json.Unmarshal(data, &creds); err == nil {
				return &creds
			}
		}
	}

	// config via env variable for CI secrets
	if payload := os.Getenv("AUDIBLE_CREDENTIALS_JSON"); payload != "" {
		var creds Credentials
		if err := json.Unmarshal([]byte(payload), &creds); err == nil {
			return &creds
		}
	}

	if adp := os.Getenv("AUDIBLE_ADP_TOKEN"); adp != "" {
		creds := &Credentials{
			ADPToken:     adp,
			AccessToken:  os.Getenv("AUDIBLE_ACCESS_TOKEN"),
			RefreshToken: os.Getenv("AUDIBLE_REFRESH_TOKEN"),
			CustomerID:   os.Getenv("AUDIBLE_CUSTOMER_ID"),
			Marketplace:  os.Getenv("AUDIBLE_MARKETPLACE"),
		}
		return creds
	}

	t.Skip("integration credentials not available")
	return nil
}

func TestIntegration_GetLibraryAndDownloadInfo(t *testing.T) {
	creds := loadIntegrationCredentials(t)
	c := NewClient(MarketplaceUS)
	c.SetCredentials(creds)

	if creds.Marketplace != "" {
		if mp, ok := GetMarketplace(creds.Marketplace); ok {
			c.SetMarketplace(mp)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	library, err := c.GetLibrary(ctx)
	if err != nil {
		t.Fatalf("GetLibrary failed: %v", err)
	}
	if len(library.Items) == 0 {
		t.Skip("integration account has no library items")
	}

	item := library.Items[0]
	info, err := c.GetDownloadInfo(ctx, item.BestID())
	if err != nil {
		t.Fatalf("GetDownloadInfo failed: %v", err)
	}
	if info.ContentURL == "" {
		t.Fatalf("GetDownloadInfo returned empty ContentURL")
	}
}
