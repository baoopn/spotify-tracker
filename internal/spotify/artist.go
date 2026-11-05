package spotify

import (
	"context"
	"net/url"
)

type artistResponse struct {
	ID     string `json:"id"`
	Images []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"images"`
}

// ArtistImage returns the URL of an artist's profile image, preferring the
// smallest image that is still at least minSize pixels wide (Spotify lists
// them largest first). It returns "" with no error when the artist has no
// image or Spotify refuses the lookup.
func (m *Manager) ArtistImage(ctx context.Context, id string) (string, error) {
	var ar artistResponse
	found, err := m.getJSON(ctx, "artist "+id, "/artists/"+url.PathEscape(id), &ar)
	if err != nil || !found {
		return "", err
	}
	return pickImage(ar, 300), nil
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
