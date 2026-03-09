/*
Package audible provides a pure Go client for the Audible API.

This library enables authentication with Audible/Amazon, accessing the user's
audiobook library, downloading content, and extracting activation bytes for
DRM removal.

# Authentication

Authentication uses OAuth 2.0 with PKCE, following the same flow as the
official Audible mobile apps. The flow works as follows:

 1. Generate an OAuth URL using [Client.GetAuthURL]
 2. User visits the URL and authenticates with Amazon
 3. Amazon redirects with an authorization code
 4. Exchange the code using [Client.Authenticate]

Example:

	client := audible.NewClient(audible.MarketplaceUS)

	authURL, _ := client.GetAuthURL()
	// Direct the user to authURL.URL to sign in with Amazon.
	// After signing in they land on a maplanding page — extract the code:
	code, _ := audible.HandleAuthRedirect(redirectURL)
	client.Authenticate(ctx, audible.DeviceRegistrationRequest{
		AuthorizationCode: code,
	})

# Library Access

Once authenticated, you can access the user's library:

	library, _ := client.GetLibrary(ctx,
		audible.WithPageSize(50),
		audible.WithSortBy("-PurchaseDate"),
	)

	for _, book := range library.Items {
		fmt.Printf("%s by %s\n", book.Title, book.Authors[0].Name)
	}

# Downloads

To download an audiobook:

	info, _ := client.GetDownloadInfo(ctx, asin)
	// info.ContentURL contains the download URL
	// info.LicenseResponse contains AAXC decryption keys (if applicable)

# Activation Bytes

For AAX file decryption with FFmpeg:

	activation, _ := client.GetActivationBytes(ctx)
	// Use: ffmpeg -activation_bytes <bytes> -i book.aax -c copy book.m4b

# Marketplaces

The library supports all Audible marketplaces:

	audible.MarketplaceUS  // United States
	audible.MarketplaceUK  // United Kingdom
	audible.MarketplaceDE  // Germany
	audible.MarketplaceFR  // France
	audible.MarketplaceAU  // Australia
	// ... and more

Use [GetMarketplace] to look up by country code, or [AllMarketplaces] to
list all available marketplaces.

# Credential Storage

Credentials can be serialized for storage:

	// Save
	data, _ := client.MarshalCredentials()
	os.WriteFile("creds.json", data, 0600)

	// Load
	data, _ := os.ReadFile("creds.json")
	client.UnmarshalCredentials(data)

For encrypted storage, use [EncryptAES] and [DecryptAES] with a key derived
from [DeriveKey].
*/
package audible
