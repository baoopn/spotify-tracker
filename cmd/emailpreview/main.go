// Command emailpreview writes every alert email as .html, .txt and a raw
// .eml file so they can be checked in a browser or mail client. It never
// sends mail.
//
//	go run ./cmd/emailpreview -out ./email-preview -public-url https://listening.example.com
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/baoopn/spotify-tracker/internal/alerts"
	"github.com/baoopn/spotify-tracker/internal/notify"
)

func main() {
	out := flag.String("out", "email-preview", "output directory")
	publicURL := flag.String("public-url", "https://listening.example.com", "PUBLIC_URL of the site")
	logoURL := flag.String("logo-url", "", "EMAIL_LOGO_URL override (optional)")
	flag.Parse()
	if err := run(*out, *publicURL, *logoURL); err != nil {
		fmt.Fprintln(os.Stderr, "emailpreview:", err)
		os.Exit(1)
	}
}

func run(out, publicURL, logoURL string) error {
	brand, err := notify.NewBrand(publicURL, logoURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	now := time.Now()
	for _, s := range alerts.Samples(publicURL, now) {
		text, html, err := s.Email.Render(brand)
		if err != nil {
			return err
		}
		eml, err := notify.Message("spotify-tracker <alerts@example.com>", []string{"you@example.com"}, s.Email, brand, now)
		if err != nil {
			return err
		}
		for ext, body := range map[string][]byte{".html": []byte(html), ".txt": []byte(text), ".eml": eml} {
			if err := os.WriteFile(filepath.Join(out, s.Name+ext), body, 0o600); err != nil {
				return err
			}
		}
		fmt.Printf("%-16s %6d bytes HTML  %s\n", s.Name, len(html), s.Email.Subject)
	}
	return nil
}
