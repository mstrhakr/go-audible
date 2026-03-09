// Example: Download an audiobook
//
// This example demonstrates how to:
// 1. Load saved credentials
// 2. Get download information for a book
// 3. Download the book with progress tracking
//
// Run with: go run main.go <ASIN>
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/mstrhakr/go-audible"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <ASIN>")
		fmt.Println("Example: go run main.go B08G9PRS1K")
		os.Exit(1)
	}

	asin := os.Args[1]
	ctx := context.Background()

	// Create client and load credentials
	client := audible.NewClient(audible.MarketplaceUS)

	data, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatal("No credentials found. Run the basic example first to authenticate.")
	}

	if err := client.UnmarshalCredentials(data); err != nil {
		log.Fatalf("Failed to load credentials: %v", err)
	}

	// Refresh token if needed
	if err := client.RefreshAccessToken(ctx); err != nil {
		log.Fatalf("Failed to refresh token: %v", err)
	}

	// Get book info
	fmt.Printf("📖 Getting info for %s...\n", asin)
	book, err := client.GetBook(ctx, asin)
	if err != nil {
		log.Fatalf("Failed to get book: %v", err)
	}

	fmt.Printf("   Title: %s\n", book.Title)
	if len(book.Authors) > 0 {
		fmt.Printf("   Author: %s\n", book.Authors[0].Name)
	}

	// Get download info
	fmt.Println("\n🔗 Getting download URL...")
	downloadInfo, err := client.GetDownloadInfo(ctx, asin)
	if err != nil {
		log.Fatalf("Failed to get download info: %v", err)
	}

	fmt.Printf("   Content type: %s\n", downloadInfo.ContentType)
	fmt.Printf("   Size: %.2f MB\n", float64(downloadInfo.ContentMetadata.ContentReference.ContentSizeInBytes)/1024/1024)

	if downloadInfo.LicenseResponse != nil {
		fmt.Println("   License: AAXC (voucher-based decryption)")
		fmt.Printf("   Key: %s\n", downloadInfo.LicenseResponse.Key)
		fmt.Printf("   IV: %s\n", downloadInfo.LicenseResponse.IV)
	} else {
		fmt.Println("   License: AAX (activation bytes decryption)")
	}

	// Get chapters
	fmt.Println("\n📑 Chapters:")
	chapters, err := client.GetChapters(ctx, asin)
	if err != nil {
		fmt.Printf("   Failed to get chapters: %v\n", err)
	} else {
		for i, ch := range chapters.Chapters {
			if i >= 5 {
				fmt.Printf("   ... and %d more chapters\n", len(chapters.Chapters)-5)
				break
			}
			ms := ch.StartOffsetMs
			hours := ms / 3600000
			ms %= 3600000
			minutes := ms / 60000
			ms %= 60000
			seconds := ms / 1000
			fmt.Printf("   %02d:%02d:%02d - %s\n", hours, minutes, seconds, ch.Title)
		}
	}

	// Ask to download
	fmt.Print("\nDownload this book? [y/N]: ")
	var answer string
	fmt.Scanln(&answer)

	if answer != "y" && answer != "Y" {
		fmt.Println("Download cancelled.")
		return
	}

	// Create download writer
	outputPath := filepath.Join(".", sanitizeFilename(book.Title)+".aax")
	writer, err := NewFileDownloadWriter(outputPath)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer writer.Close()

	fmt.Printf("\n⬇️  Downloading to %s...\n", outputPath)

	bytesWritten, err := client.DownloadBook(ctx, asin, writer)
	if err != nil {
		log.Fatalf("Download failed: %v", err)
	}

	fmt.Printf("\n✓ Downloaded %.2f MB\n", float64(bytesWritten)/1024/1024)
	fmt.Println()
	fmt.Println("To decrypt, use FFmpeg:")

	if downloadInfo.LicenseResponse != nil {
		fmt.Printf("  ffmpeg -audible_key %s -audible_iv %s -i %s -c copy %s.m4b\n",
			downloadInfo.LicenseResponse.Key,
			downloadInfo.LicenseResponse.IV,
			outputPath,
			sanitizeFilename(book.Title))
	} else {
		// Get activation bytes
		activation, err := client.GetActivationBytes(ctx)
		if err != nil {
			fmt.Println("  (Failed to get activation bytes)")
		} else {
			fmt.Printf("  ffmpeg -activation_bytes %s -i %s -c copy %s.m4b\n",
				activation.ActivationBytes,
				outputPath,
				sanitizeFilename(book.Title))
		}
	}
}

// FileDownloadWriter implements audible.DownloadWriter for file downloads.
type FileDownloadWriter struct {
	file    *os.File
	info    *audible.DownloadInfo
	lastPct int
}

func NewFileDownloadWriter(path string) (*FileDownloadWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &FileDownloadWriter{file: f}, nil
}

func (w *FileDownloadWriter) OnStart(asin string, contentLength int64, info *audible.DownloadInfo) error {
	w.info = info
	return nil
}

func (w *FileDownloadWriter) Write(p []byte) (int, error) {
	return w.file.Write(p)
}

func (w *FileDownloadWriter) OnProgress(bytesWritten, totalBytes int64) error {
	if totalBytes <= 0 {
		return nil
	}

	pct := int(bytesWritten * 100 / totalBytes)
	if pct != w.lastPct && pct%10 == 0 {
		fmt.Printf("   Progress: %d%%\n", pct)
		w.lastPct = pct
	}
	return nil
}

func (w *FileDownloadWriter) OnComplete() error {
	return nil
}

func (w *FileDownloadWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Ensure FileDownloadWriter implements the interface
var _ io.WriteCloser = (*FileDownloadWriter)(nil)

func sanitizeFilename(name string) string {
	// Remove characters that are invalid in filenames
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	result := name
	for _, ch := range invalid {
		result = replaceAll(result, ch, "")
	}
	return result
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx == -1 {
			break
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
	return s
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
