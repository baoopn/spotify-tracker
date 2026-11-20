package spotify

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Caps for the copyrights kept per album.
const (
	maxCopyrights   = 10
	maxCopyrightLen = 500
)

type albumResponse struct {
	AlbumType            string `json:"album_type"`
	TotalTracks          int32  `json:"total_tracks"`
	ReleaseDatePrecision string `json:"release_date_precision"`
	ExternalIDs          struct {
		UPC string `json:"upc"`
	} `json:"external_ids"`
	Copyrights []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"copyrights"`
	Tracks struct {
		Items []struct {
			ID          string `json:"id"`
			TrackNumber int32  `json:"track_number"`
			DiscNumber  int32  `json:"disc_number"`
			LinkedFrom  *struct {
				ID string `json:"id"`
			} `json:"linked_from"`
		} `json:"items"`
	} `json:"tracks"`
}

// AlbumDetails reads GET /albums/{id}: type, size, release date precision,
// UPC, copyrights and the track numbers of the first page of tracks (up to
// 50; later pages are ignored). It returns nil, nil when Spotify refuses the
// lookup (403 or 404).
func (m *Manager) AlbumDetails(ctx context.Context, id string) (*domain.AlbumDetails, error) {
	var ar albumResponse
	code, err := m.getJSONCode(ctx, "album "+id, "/albums/"+url.PathEscape(id), &ar)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		m.logRefused(ctx, "album "+id, code)
		return nil, nil
	}
	d := &domain.AlbumDetails{
		AlbumType:            domain.NormalizeAlbumType(ar.AlbumType),
		ReleaseDatePrecision: domain.NormalizeReleaseDatePrecision(ar.ReleaseDatePrecision),
		UPC:                  strings.TrimSpace(ar.ExternalIDs.UPC),
		TotalTracks:          domain.PositiveInt32(ar.TotalTracks),
		Copyrights:           []domain.Copyright{},
	}
	for _, c := range ar.Copyrights {
		text := truncateRunes(strings.TrimSpace(c.Text), maxCopyrightLen)
		if text == "" {
			continue
		}
		d.Copyrights = append(d.Copyrights, domain.Copyright{
			Text: text, Type: strings.ToUpper(strings.TrimSpace(c.Type))})
		if len(d.Copyrights) == maxCopyrights {
			break
		}
	}
	for _, it := range ar.Tracks.Items {
		tn, dn := domain.PositiveInt32(it.TrackNumber), domain.PositiveInt32(it.DiscNumber)
		if it.ID != "" {
			d.Tracks = append(d.Tracks, domain.AlbumTrackNumber{TrackID: it.ID, TrackNumber: tn, DiscNumber: dn})
		}
		// A relinked track is stored under the ID that was played.
		if it.LinkedFrom != nil && it.LinkedFrom.ID != "" && it.LinkedFrom.ID != it.ID {
			d.Tracks = append(d.Tracks, domain.AlbumTrackNumber{TrackID: it.LinkedFrom.ID, TrackNumber: tn, DiscNumber: dn})
		}
	}
	return d, nil
}

// truncateRunes cuts s to at most n runes.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
