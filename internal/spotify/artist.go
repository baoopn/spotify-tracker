package spotify

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Caps for the genres kept per artist.
const (
	maxArtistGenres = 20
	maxGenreLen     = 100
)

type artistResponse struct {
	ID     string `json:"id"`
	Images []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"images"`
	Genres []string `json:"genres"`
}

// ArtistProfile is the part of GET /artists/{id} the tracker keeps.
type ArtistProfile struct {
	Found    bool
	ImageURL string
	Genres   []string // non-nil when Found (may be empty)
}

// ArtistProfile looks up an artist's profile image and genres. The image is
// the smallest one that is still at least 300 pixels wide (Spotify lists them
// largest first), or "" when the artist has none. Found is false (with no
// error) when Spotify refuses the lookup (403 or 404).
func (m *Manager) ArtistProfile(ctx context.Context, id string) (ArtistProfile, error) {
	var ar artistResponse
	code, err := m.getJSONCode(ctx, "artist "+id, "/artists/"+url.PathEscape(id), &ar)
	if err != nil {
		return ArtistProfile{}, err
	}
	if code != http.StatusOK {
		m.logRefused(ctx, "artist "+id, code)
		return ArtistProfile{}, nil
	}
	return ArtistProfile{Found: true, ImageURL: pickImage(ar, 300), Genres: cleanGenres(ar.Genres)}, nil
}

// cleanGenres trims, drops empty, overlong and duplicate genres (keeping the
// order) and caps the list. The result is never nil.
func cleanGenres(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || utf8.RuneCountInString(g) > maxGenreLen || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
		if len(out) == maxArtistGenres {
			break
		}
	}
	return out
}

func pickImage(ar artistResponse, minSize int) string {
	best := ""
	bestW := 0
	for _, img := range ar.Images {
		if img.URL == "" {
			continue
		}
		switch {
		case best == "":
			best, bestW = img.URL, img.Width
		case img.Width >= minSize && (bestW < minSize || img.Width < bestW):
			best, bestW = img.URL, img.Width
		}
	}
	return best
}
