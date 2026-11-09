package store

import (
	"fmt"
	"regexp"
	"strings"
)

// Spotify IDs are exactly 22 base62 characters (case-sensitive).
var (
	spotifyBareID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)
	spotifyURI    = regexp.MustCompile(`^spotify:(track|artist|album):([0-9A-Za-z]{22})$`)
	spotifyURL    = regexp.MustCompile(`^(?i:https?://(?:www\.)?open\.spotify\.com)/` +
		`(?:intl-[A-Za-z]{2}(?:-[A-Za-z]{2})?/)?(?:embed/)?` +
		`(track|artist|album)/([0-9A-Za-z]{22})/?(?:[?#][^\s]*)?$`)
)

// spotifyRef recognises a Spotify ID in a search term: a bare 22-char base62
// ID (kind ""), a URI spotify:{track|artist|album}:ID, or an open.spotify.com
// URL (http or https, optional www, intl-xx and embed/ segments, query and
// fragment ignored). Anything else returns ok=false.
func spotifyRef(q string) (kind, id string, ok bool) {
	q = strings.TrimSpace(q)
	if spotifyBareID.MatchString(q) {
		return "", q, true
	}
	if m := spotifyURI.FindStringSubmatch(q); m != nil {
		return m[1], m[2], true
	}
	if m := spotifyURL.FindStringSubmatch(q); m != nil {
		return m[1], m[2], true
	}
	return "", "", false
}

// trackIDMatch returns the exact-ID conditions on the track aliased t for the
// Spotify ID bound to parameter $n: the track itself, tracks by that artist,
// and tracks on that album. kind "" (bare ID) checks all three.
func trackIDMatch(kind string, n int) []string {
	var c []string
	if kind == "" || kind == "track" {
		c = append(c, fmt.Sprintf("t.id = $%d", n))
	}
	if kind == "" || kind == "artist" {
		c = append(c, fmt.Sprintf("EXISTS (SELECT 1 FROM track_artists ta WHERE ta.track_id = t.id AND ta.artist_id = $%d)", n))
	}
	if kind == "" || kind == "album" {
		c = append(c, fmt.Sprintf("t.album_id = $%d", n))
	}
	return c
}

// artistIDMatch returns the exact-ID conditions on the artist aliased a for
// the Spotify ID bound to parameter $n: the artist itself, artists of that
// track, and artists on tracks of that album.
func artistIDMatch(kind string, n int) []string {
	var c []string
	if kind == "" || kind == "artist" {
		c = append(c, fmt.Sprintf("a.id = $%d", n))
	}
	if kind == "" || kind == "track" {
		c = append(c, fmt.Sprintf("EXISTS (SELECT 1 FROM track_artists ta WHERE ta.artist_id = a.id AND ta.track_id = $%d)", n))
	}
	if kind == "" || kind == "album" {
		c = append(c, fmt.Sprintf("EXISTS (SELECT 1 FROM track_artists ta JOIN tracks t ON t.id = ta.track_id "+
			"WHERE ta.artist_id = a.id AND t.album_id = $%d)", n))
	}
	return c
}

// orIDs appends " OR cond" for each condition (empty when there are none).
func orIDs(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " OR " + strings.Join(conds, " OR ")
}
