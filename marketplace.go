package audible

// Marketplace represents an Audible regional marketplace.
type Marketplace struct {
	Name          string
	CountryCode   string
	Domain        string // Top-level domain only, e.g. "com", "co.uk", "de"
	MarketplaceID string
}

// AmazonDomain returns the full Amazon domain (e.g. "amazon.com").
func (m Marketplace) AmazonDomain() string {
	return "amazon." + m.Domain
}

// AudibleDomain returns the full Audible domain (e.g. "audible.com").
func (m Marketplace) AudibleDomain() string {
	return "audible." + m.Domain
}

// APIEndpoint returns the Audible API endpoint URL.
func (m Marketplace) APIEndpoint() string {
	return "https://api.audible." + m.Domain
}

// Predefined marketplaces
var (
	MarketplaceUS = Marketplace{
		Name:          "United States",
		CountryCode:   "us",
		Domain:        "com",
		MarketplaceID: "AF2M0KC94RCEA",
	}

	MarketplaceUK = Marketplace{
		Name:          "United Kingdom",
		CountryCode:   "uk",
		Domain:        "co.uk",
		MarketplaceID: "A2I9A3Q2GNFNGQ",
	}

	MarketplaceDE = Marketplace{
		Name:          "Germany",
		CountryCode:   "de",
		Domain:        "de",
		MarketplaceID: "AN7V1F1VY261K",
	}

	MarketplaceFR = Marketplace{
		Name:          "France",
		CountryCode:   "fr",
		Domain:        "fr",
		MarketplaceID: "A2728XDNODOQ8T",
	}

	MarketplaceAU = Marketplace{
		Name:          "Australia",
		CountryCode:   "au",
		Domain:        "com.au",
		MarketplaceID: "AN7EY7DTAW63G",
	}

	MarketplaceCA = Marketplace{
		Name:          "Canada",
		CountryCode:   "ca",
		Domain:        "ca",
		MarketplaceID: "A2CQZ5RBY40XE",
	}

	MarketplaceIT = Marketplace{
		Name:          "Italy",
		CountryCode:   "it",
		Domain:        "it",
		MarketplaceID: "A2N7FU2W2BU2ZC",
	}

	MarketplaceIN = Marketplace{
		Name:          "India",
		CountryCode:   "in",
		Domain:        "in",
		MarketplaceID: "AJO3FBRUE6J4S",
	}

	MarketplaceJP = Marketplace{
		Name:          "Japan",
		CountryCode:   "jp",
		Domain:        "co.jp",
		MarketplaceID: "A1QAP3MOU4173J",
	}

	MarketplaceES = Marketplace{
		Name:          "Spain",
		CountryCode:   "es",
		Domain:        "es",
		MarketplaceID: "ALMIKO4SZCSAR",
	}

	MarketplaceBR = Marketplace{
		Name:          "Brazil",
		CountryCode:   "br",
		Domain:        "com.br",
		MarketplaceID: "A10J1VAYUDTYRN",
	}
)

// AllMarketplaces returns all available marketplaces.
func AllMarketplaces() []Marketplace {
	return []Marketplace{
		MarketplaceUS,
		MarketplaceUK,
		MarketplaceDE,
		MarketplaceFR,
		MarketplaceAU,
		MarketplaceCA,
		MarketplaceIT,
		MarketplaceIN,
		MarketplaceJP,
		MarketplaceES,
		MarketplaceBR,
	}
}

// GetMarketplace returns a marketplace by country code.
func GetMarketplace(countryCode string) (Marketplace, bool) {
	for _, m := range AllMarketplaces() {
		if m.CountryCode == countryCode {
			return m, true
		}
	}
	return Marketplace{}, false
}

// OAuth configuration constants
const (
	// Device type identifier (Audible iOS app)
	DeviceTypeID = "A2CZJZGLK2JJVM"

	// App name for OAuth
	AppName = "Audible"

	// App version
	AppVersion = "3.56.2"

	// Software version
	SoftwareVersion = "35602678"

	// OS version
	OSVersion = "15.0.0"

	// Device model
	DeviceModel = "iPhone"

	// OAuth client ID
	OAuthClientID = "amzn1.application-oa2-client.4dba4e9f040d41ea89dc41053a8e5f7d"
)
