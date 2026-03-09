# go-audible

A pure Go library for authenticating with Audible and accessing the Audible API.

## Features

- OAuth2 authentication with Amazon/Audible (PKCE flow)
- Device registration and credential management
- Request signing (SHA256withRSA)
- Activation bytes extraction for AAX decryption
- Library access (list books, metadata)
- Content download URLs
- Multi-marketplace support (US, UK, DE, etc.)

## Installation

```bash
go get github.com/mstrhakr/go-audible
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/mstrhakr/go-audible"
)

func main() {
    // Create a new client for US marketplace
    client := audible.NewClient(audible.MarketplaceUS)

    // Start OAuth flow - returns URL for user to visit
    authURL, err := client.GetAuthURL()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Visit this URL to authenticate:", authURL)

    // After user authenticates, they'll be redirected with a code
    // Exchange the code for credentials
    var authCode string
    fmt.Print("Enter the authorization code: ")
    fmt.Scanln(&authCode)

    if err := client.Authenticate(context.Background(), authCode); err != nil {
        log.Fatal(err)
    }

    // Get user's library
    library, err := client.GetLibrary(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    for _, book := range library.Items {
        fmt.Printf("%s by %s\n", book.Title, book.Authors[0].Name)
    }

    // Get activation bytes for AAX decryption
    activationBytes, err := client.GetActivationBytes(context.Background())
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Activation bytes:", activationBytes)
}
```

## Authentication Flow

1. Call `GetAuthURL()` to generate an OAuth URL
2. User visits the URL and logs into Amazon
3. Amazon redirects to callback URL with authorization code
4. Call `Authenticate(code)` to exchange code for credentials
5. Credentials are stored and used for subsequent API calls

## Supported Marketplaces

| Marketplace | Domain | Country |
| ----------- | ------ | ------- |
| `MarketplaceUS` | audible.com | United States |
| `MarketplaceUK` | audible.co.uk | United Kingdom |
| `MarketplaceDE` | audible.de | Germany |
| `MarketplaceFR` | audible.fr | France |
| `MarketplaceAU` | audible.com.au | Australia |
| `MarketplaceCA` | audible.ca | Canada |
| `MarketplaceIT` | audible.it | Italy |
| `MarketplaceIN` | audible.in | India |
| `MarketplaceJP` | audible.co.jp | Japan |

## API Reference

### Client

```go
// Create client for a marketplace
client := audible.NewClient(audible.MarketplaceUS)

// Load existing credentials
client.LoadCredentials("path/to/credentials.json")

// Save credentials for later use
client.SaveCredentials("path/to/credentials.json")
```

### Library

```go
// Get full library
library, err := client.GetLibrary(ctx)

// Get library with options
library, err := client.GetLibrary(ctx, 
    audible.WithResponseGroups("product_desc", "contributors", "series"),
    audible.WithPageSize(50),
)

// Get single book
book, err := client.GetBook(ctx, "B08G9PRS1K")
```

### Downloads

```go
// Get download URL for a book
download, err := client.GetDownloadURL(ctx, "B08G9PRS1K")
fmt.Println(download.ContentURL)

// Get activation bytes for decryption
activationBytes, err := client.GetActivationBytes(ctx)
// Use with FFmpeg: ffmpeg -activation_bytes <bytes> -i book.aax -c copy book.m4b
```

## License

MIT
