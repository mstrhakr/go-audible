# go-audible

Pure Go client for Audible authentication and API access.

This library supports OAuth + device registration, signed Audible API requests,
library browsing, download URL + license retrieval, chapter metadata, and AAX
activation bytes extraction.

## Features

- OAuth authentication with Amazon/Audible using PKCE
- Device registration and token refresh
- Audible request signing (`SHA256withRSA`)
- Library APIs (`GetLibrary`, `GetAllLibrary`, `GetBook`)
- Download/license APIs (`GetDownloadInfo`, `GetChapters`, `DownloadBook`)
- AAX activation bytes extraction (`GetActivationBytes`)
- Multi-marketplace support (US, UK, DE, FR, AU, CA, IT, IN, JP, ES, BR)

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
    ctx := context.Background()
    client := audible.NewClient(audible.MarketplaceUS)

    // 1) Generate sign-in URL
    authURL, err := client.GetAuthURL()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Visit:", authURL.URL)

    // 2) Paste full redirect URL from the browser after sign-in
    var redirectURL string
    fmt.Print("Redirect URL: ")
    fmt.Scanln(&redirectURL)

    code, err := audible.HandleAuthRedirect(redirectURL)
    if err != nil {
        log.Fatal(err)
    }

    // 3) Exchange auth code for credentials
    err = client.Authenticate(ctx, audible.DeviceRegistrationRequest{
        AuthorizationCode: code,
        CodeVerifier:      authURL.CodeVerifier,
        DeviceSerial:      authURL.DeviceSerial,
    })
    if err != nil {
        log.Fatal(err)
    }

    // 4) Query library
    library, err := client.GetLibrary(ctx, audible.WithPageSize(10))
    if err != nil {
        log.Fatal(err)
    }

    for _, book := range library.Items {
        author := "Unknown"
        if len(book.Authors) > 0 {
            author = book.Authors[0].Name
        }
        fmt.Printf("%s by %s\n", book.Title, author)
    }
}
```

## Authentication Flow

1. Call `GetAuthURL()`.
2. User signs in at `authURL.URL`.
3. Capture full redirect URL from the browser.
4. Parse auth code with `HandleAuthRedirect(redirectURL)`.
5. Call `Authenticate(ctx, DeviceRegistrationRequest{...})`.
6. Persist credentials with `MarshalCredentials()` for reuse.

## Credential Persistence

```go
// Save credentials
data, err := client.MarshalCredentials()
if err != nil {
    return err
}
err = os.WriteFile("credentials.json", data, 0o600)

// Load credentials
data, err = os.ReadFile("credentials.json")
if err != nil {
    return err
}
err = client.UnmarshalCredentials(data)
```

Note: `LoadCredentials` and `SaveCredentials` are currently placeholders and
return `not implemented`.

## Library API

```go
// Paged library
library, err := client.GetLibrary(ctx,
    audible.WithPageSize(50),
    audible.WithSortBy("-PurchaseDate"),
)

// Entire library (auto-pagination)
allBooks, err := client.GetAllLibrary(ctx)

// Single book
book, err := client.GetBook(ctx, "B08G9PRS1K")
```

## Download API

```go
// Download metadata + license info
info, err := client.GetDownloadInfo(ctx, "B08G9PRS1K")
if err != nil {
    return err
}
fmt.Println("Content URL:", info.ContentURL)

// Chapters
chapters, err := client.GetChapters(ctx, "B08G9PRS1K")

// Stream download using your DownloadWriter implementation
_, err = client.DownloadBook(ctx, "B08G9PRS1K", writer)
```

For AAXC content, `info.LicenseResponse` may contain `Key` and `IV` for decryption.

## Activation Bytes (AAX)

```go
activation, err := client.GetActivationBytes(ctx)
if err != nil {
    return err
}
fmt.Println("Activation bytes:", activation.ActivationBytes)
// ffmpeg -activation_bytes <bytes> -i book.aax -c copy book.m4b
```

## Supported Marketplaces

| Marketplace | Domain | Country |
| --- | --- | --- |
| `MarketplaceUS` | audible.com | United States |
| `MarketplaceUK` | audible.co.uk | United Kingdom |
| `MarketplaceDE` | audible.de | Germany |
| `MarketplaceFR` | audible.fr | France |
| `MarketplaceAU` | audible.com.au | Australia |
| `MarketplaceCA` | audible.ca | Canada |
| `MarketplaceIT` | audible.it | Italy |
| `MarketplaceIN` | audible.in | India |
| `MarketplaceJP` | audible.co.jp | Japan |
| `MarketplaceES` | audible.es | Spain |
| `MarketplaceBR` | audible.com.br | Brazil |

Use `GetMarketplace("us")` or `AllMarketplaces()` for lookup/discovery.

## Examples

- `examples/basic`: authentication + library listing
- `examples/download`: fetch metadata + download a title

## License

MIT
