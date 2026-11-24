package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// lyricsStore reads lyrics (implemented by *store.Store).
type lyricsStore interface {
	TrackLyrics(ctx context.Context, id string, withSynced bool) (store.TrackLyrics, error)
	SearchLyrics(ctx context.Context, q string, limit int) ([]store.LyricsMatch, error)
}

func (a *API) lyricsDB() lyricsStore {
	if a.lyrics != nil {
		return a.lyrics
	}
	return a.Store
}

// trackLyrics serves GET /tracks/{id}/lyrics[?synced=true].
func (a *API) trackLyrics(w http.ResponseWriter, r *http.Request) {
	withSynced, ok := boolParam(r.URL.Query().Get("synced"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_synced", "synced must be true or false")
		return
	}
	l, err := a.lyricsDB().TrackLyrics(r.Context(), chi.URLParam(r, "id"), withSynced)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

// searchLyrics serves GET /lyrics/search?q=&limit=.
func (a *API) searchLyrics(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 200 || !utf8.ValidString(q) ||
		strings.ContainsRune(q, 0) || !strings.ContainsFunc(q, isWordRune) {
		writeError(w, http.StatusBadRequest, "invalid_query",
			"q must be 2 to 200 characters of valid text and contain a letter or digit")
		return
	}
	limit, err := intParam(r, "limit", 10, 1, 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	rows, err := a.lyricsDB().SearchLyrics(r.Context(), q, limit)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if rows == nil {
		rows = []store.LyricsMatch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// boolParam parses an optional boolean query value ("" = false).
func boolParam(v string) (value, ok bool) {
	switch strings.ToLower(v) {
	case "", "false", "0":
		return false, true
	case "true", "1":
		return true, true
	}
	return false, false
}
