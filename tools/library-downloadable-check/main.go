package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mstrhakr/go-audible"
)

type resultRow struct {
	ASIN             string `json:"asin"`
	Title            string `json:"title"`
	APIDownloadable  bool   `json:"api_downloadable"`
	ComputedDownload bool   `json:"computed_downloadable"`
	ContentType      string `json:"content_type"`
	FormatType       string `json:"format_type"`
	DeliveryType     string `json:"content_delivery_type"`
	Mismatch         bool   `json:"mismatch"`
}

func main() {
	credPath := flag.String("credentials", "credentials.json", "Path to JSON credentials file (from client.MarshalCredentials())")
	marketplace := flag.String("marketplace", "us", "Audible marketplace code (us, uk, de, fr, au, ca, it, in, jp, es, br)")
	format := flag.String("format", "table", "Output format: table, json, md")
	asin := flag.String("asin", "", "Optional single ASIN to inspect")
	onlyMismatch := flag.Bool("only-mismatch", false, "Only show books where API downloadable disagrees with local computed status")
	pageSize := flag.Int("page-size", 50, "Number of library items per page to request")
	limit := flag.Int("limit", 0, "Optional limit on number of library books to process (0 for no limit)")
	flag.Parse()

	market, ok := audible.GetMarketplace(strings.ToLower(strings.TrimSpace(*marketplace)))
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported marketplace: %s\n", *marketplace)
		os.Exit(2)
	}

	data, err := os.ReadFile(*credPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read credentials file: %v\n", err)
		os.Exit(1)
	}

	client := audible.NewClient(market)
	if err := client.UnmarshalCredentials(data); err != nil {
		// Try to handle nested payload where credentials may be under a key.
		var wrapped map[string]json.RawMessage
		if err2 := json.Unmarshal(data, &wrapped); err2 == nil {
			if inner, ok := wrapped["credentials"]; ok {
				if err3 := client.UnmarshalCredentials(inner); err3 == nil {
					goto ready
				}
			}
		}
		fmt.Fprintf(os.Stderr, "failed to unmarshal credentials: %v\n", err)
		os.Exit(1)
	}

ready:
	if !client.IsAuthenticated() {
		fmt.Fprintf(os.Stderr, "client is not authenticated after loading credentials\n")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	books := []audible.Book{}

	if *asin != "" {
		book, err := client.GetBook(ctx, *asin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "GetBook failed: %v\n", err)
			os.Exit(1)
		}
		books = append(books, *book)
	} else {
		opts := []audible.LibraryOption{audible.WithPageSize(*pageSize)}
		all, err := client.GetAllLibrary(ctx, opts...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "GetAllLibrary failed: %v\n", err)
			os.Exit(1)
		}
		books = all
	}

	if len(books) == 0 {
		fmt.Println("No books found in library")
		return
	}

	results := make([]resultRow, 0, len(books))
	matchCount := 0
	mismatchCount := 0
	skippedCount := 0

	for i, b := range books {
		if *limit > 0 && i >= *limit {
			break
		}

		api := b.IsDownloadable
		calc := b.Downloadable()
		if api == calc {
			matchCount++
		} else {
			mismatchCount++
		}

		if *onlyMismatch && api == calc {
			skippedCount++
			continue
		}

		results = append(results, resultRow{
			ASIN:             b.BestID(),
			Title:            strings.TrimSpace(b.Title),
			APIDownloadable:  api,
			ComputedDownload: calc,
			ContentType:      b.ContentType,
			FormatType:       b.FormatType,
			DeliveryType:     b.ContentDeliveryType,
			Mismatch:         api != calc,
		})
	}

	if *format == "json" {
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to marshal JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
		return
	}

	fmt.Printf("Found %d books (marketplace=%s)\n", len(results), market.CountryCode)

	if *format == "md" {
		fmt.Println("| ASIN | Title | API | Computed | ContentType | FormatType | DeliveryType | Mismatch |")
		fmt.Println("|---|---|---|---|---|---|---|---|")
		for _, r := range results {
			fmt.Printf("| %s | %s | %t | %t | %s | %s | %s | %t |\n",
				r.ASIN, escapeMarkdown(r.Title), r.APIDownloadable, r.ComputedDownload,
				r.ContentType, r.FormatType, r.DeliveryType, r.Mismatch)
		}
		fmt.Println()
	} else {
		printHeader()
		for _, r := range results {
			printRow(r)
		}
	}

	fmt.Println(strings.Repeat("=", 120))
	fmt.Printf("matching=%d, mismatching=%d, skipped=%d\n", matchCount, mismatchCount, skippedCount)

	if mismatchCount > 0 {
		fmt.Printf("\nPotential problematic records where API is_downloadable and computed Downloadable() disagree.\n")
	}
}

func printHeader() {
	fmt.Printf("%-12s %-50s %-5s %-8s %-12s %-10s %-15s %-6s\n", "ASIN", "Title", "API", "COMPUTED", "CONTENT_TYPE", "FORMAT", "DELIVERY_TYPE", "MISMATCH")
	fmt.Println(strings.Repeat("-", 140))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func printBook(b audible.Book, api, calc bool) {
	id := b.BestID()
	if id == "" {
		id = "(unknown)"
	}
	fmt.Printf("%-12s %-50s %-5t %-8t %-12q %-10q %-15q\n",
		id,
		truncate(strings.TrimSpace(b.Title), 50),
		api,
		calc,
		b.ContentType,
		b.FormatType,
		b.ContentDeliveryType,
	)
}

func printRow(r resultRow) {
	fmt.Printf("%-12s %-50s %-5t %-8t %-12q %-10q %-15q %-6t\n",
		r.ASIN,
		truncate(r.Title, 50),
		r.APIDownloadable,
		r.ComputedDownload,
		r.ContentType,
		r.FormatType,
		r.DeliveryType,
		r.Mismatch,
	)
}

func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer("|", "\\|", "\n", " ")
	return replacer.Replace(s)
}
