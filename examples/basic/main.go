// Example: Basic authentication and library listing
//
// This example demonstrates how to:
// 1. Create a client for the US marketplace
// 2. Generate an OAuth URL for authentication
// 3. Exchange the authorization code for credentials
// 4. List books in the user's library
//
// Run with: go run main.go
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mstrhakr/go-audible"
)

func main() {
	ctx := context.Background()

	// Create a client for the US marketplace
	client := audible.NewClient(audible.MarketplaceUS)

	// Check if we have saved credentials
	credsFile := "credentials.json"
	if data, err := os.ReadFile(credsFile); err == nil {
		if err := client.UnmarshalCredentials(data); err == nil {
			fmt.Println("Loaded existing credentials")

			// Refresh token if needed
			if err := client.RefreshAccessToken(ctx); err != nil {
				fmt.Printf("Warning: Failed to refresh token: %v\n", err)
			}
		}
	}

	// If not authenticated, go through OAuth flow
	if !client.IsAuthenticated() {
		if err := authenticate(ctx, client); err != nil {
			log.Fatalf("Authentication failed: %v", err)
		}

		// Save credentials for next time
		if data, err := client.MarshalCredentials(); err == nil {
			os.WriteFile(credsFile, data, 0600)
			fmt.Println("Credentials saved")
		}
	}

	// List library
	fmt.Println("\n📚 Your Audible Library:")
	fmt.Println(strings.Repeat("-", 60))

	library, err := client.GetLibrary(ctx,
		audible.WithPageSize(10),
		audible.WithSortBy("-PurchaseDate"),
	)
	if err != nil {
		log.Fatalf("Failed to get library: %v", err)
	}

	fmt.Printf("Total books: %d\n\n", library.TotalResults)

	for _, book := range library.Items {
		author := "Unknown"
		if len(book.Authors) > 0 {
			author = book.Authors[0].Name
		}

		duration := time.Duration(book.RuntimeMinutes) * time.Minute
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60

		fmt.Printf("📖 %s\n", book.Title)
		fmt.Printf("   Author: %s\n", author)
		fmt.Printf("   Duration: %dh %dm\n", hours, minutes)

		if len(book.Series) > 0 {
			fmt.Printf("   Series: %s (Book %s)\n", book.Series[0].Title, book.Series[0].Sequence)
		}

		fmt.Println()
	}

	// Get activation bytes
	fmt.Println("🔑 Getting activation bytes...")
	activation, err := client.GetActivationBytes(ctx)
	if err != nil {
		fmt.Printf("   Failed: %v\n", err)
	} else {
		fmt.Printf("   Activation bytes: %s\n", activation.ActivationBytes)
		fmt.Println("   Use with FFmpeg: ffmpeg -activation_bytes", activation.ActivationBytes, "-i book.aax -c copy book.m4b")
	}
}

func authenticate(ctx context.Context, client *audible.Client) error {
	reader := bufio.NewReader(os.Stdin)

	// Generate OAuth URL
	authURL, err := client.GetAuthURL()
	if err != nil {
		return fmt.Errorf("failed to generate auth URL: %w", err)
	}

	fmt.Println("\n🔐 Authentication Required")
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("Please visit this URL to authenticate:")
	fmt.Println()
	fmt.Println(authURL.URL)
	fmt.Println()
	fmt.Println("After logging in, you'll be redirected to a page.")
	fmt.Println("Copy the ENTIRE URL from your browser's address bar")
	fmt.Println("and paste it below.")
	fmt.Println()
	fmt.Print("Redirect URL: ")

	redirectURL, _ := reader.ReadString('\n')
	redirectURL = strings.TrimSpace(redirectURL)

	if redirectURL == "" {
		return fmt.Errorf("no redirect URL provided")
	}

	// Extract authorization code from redirect URL
	code, err := audible.HandleAuthRedirect(redirectURL)
	if err != nil {
		return fmt.Errorf("failed to extract auth code: %w", err)
	}

	// Exchange code for credentials
	fmt.Println("\n🔄 Exchanging code for credentials...")
	err = client.Authenticate(ctx, audible.DeviceRegistrationRequest{
		AuthorizationCode: code,
		CodeVerifier:      authURL.CodeVerifier,
		DeviceSerial:      authURL.DeviceSerial,
	})
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println("✓ Authentication successful!")
	return nil
}
