package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/lrc"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const (
	// maxManualText caps each manual lyrics text, like LRCLIB lyrics.
	maxManualText = 200_000
	// maxManualBody bounds the PUT body (two texts, JSON-escaped).
	maxManualBody = 1 << 20
	// manualSlackMS is how far past the track's end a synced line may start.
	manualSlackMS = 5_000
)

// manualLyricsRequest is the body of PUT /tracks/{id}/lyrics.
type manualLyricsRequest struct {
	Synced       *string `json:"synced"`
	Plain        *string `json:"plain"`
	Instrumental bool    `json:"instrumental"`
}

// lyricsError is a 400 for manual lyrics; Lines holds per-line LRC errors.
func lyricsError(w http.ResponseWriter, code, msg string, lines []lrc.LineError) {
	writeJSON(w, http.StatusBadRequest, map[string]apiError{"error": {Code: code, Message: msg, Lines: lines}})
}

// manualLyrics validates and normalizes a manual lyrics request against the
// track's duration (ms, 0 = unknown). It returns the lyrics to store, or the
// error code, message and per-line errors for a 400.
func manualLyrics(req manualLyricsRequest, durationMS int64) (domain.Lyrics, string, string, []lrc.LineError) {
	var synced, plain string
	if req.Synced != nil {
		synced = lrc.Normalize(*req.Synced)
	}
	if req.Plain != nil {
		plain = lrc.Normalize(*req.Plain)
	}
	if len(synced) > maxManualText || len(plain) > maxManualText {
		return domain.Lyrics{}, "too_large", fmt.Sprintf("each lyrics text is at most %d bytes", maxManualText), nil
	}
	if req.Instrumental {
		if strings.TrimSpace(synced) != "" || strings.TrimSpace(plain) != "" {
			return domain.Lyrics{}, "invalid_lyrics", "instrumental lyrics take no text", nil
		}
		return domain.Lyrics{Status: domain.LyricsInstrumental}, "", "", nil
	}
	if strings.TrimSpace(synced) == "" && strings.TrimSpace(plain) == "" {
		return domain.Lyrics{}, "invalid_lyrics", "add synced or plain lyrics, or mark the song instrumental", nil
	}
	if strings.TrimSpace(synced) != "" {
		if errs := lrc.Validate(synced, durationMS, manualSlackMS); len(errs) > 0 {
			msg := "the synced lyrics have 1 error"
			if len(errs) > 1 {
				msg = fmt.Sprintf("the synced lyrics have %d errors", len(errs))
			}
			return domain.Lyrics{}, "invalid_lrc", msg, errs
		}
		if strings.TrimSpace(plain) == "" {
			plain = lrc.Plain(synced)
		}
	} else {
		synced = ""
	}
	if lrc.MarkerOnly(plain) {
		return domain.Lyrics{}, "invalid_lyrics", "the lyrics hold no words: mark the song instrumental instead", nil
	}
	return domain.Lyrics{Status: domain.LyricsFound, Plain: plain, Synced: synced}, "", "", nil
}

// putLyrics serves PUT /tracks/{id}/lyrics: stores lyrics added by hand
// (source manual). They win over LRCLIB: automatic lookups never replace
// them. Changed lyrics re-queue an existing translation first in line.
// Answers the track's lyrics (with the synced text).
func (a *API) putLyrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req manualLyricsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxManualBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_body",
			"body must be {synced?: string, plain?: string, instrumental?: boolean}")
		return
	}
	db := a.lyricsDB()
	dur, err := db.TrackDurationMS(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	l, code, msg, lines := manualLyrics(req, dur)
	if code != "" {
		lyricsError(w, code, msg, lines)
		return
	}
	actor := "system"
	if u, ok := UserFrom(r.Context()); ok {
		actor = u.Email
	}
	stored, retranslate, err := db.SetManualLyrics(r.Context(), id, l, actor)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if retranslate && a.Translation.Nudge != nil {
		a.Translation.Nudge()
	}
	if stored {
		a.audit(r, "lyrics.manual_set", map[string]any{"track_id": id, "status": l.Status,
			"synced": l.Synced != "", "retranslate": retranslate})
	}
	a.writeTrackLyrics(w, r, id, true)
}

// clearManualLyrics serves DELETE /tracks/{id}/lyrics/manual: removes the
// manual lyrics and re-queues an LRCLIB lookup at the front.
func (a *API) clearManualLyrics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ok, err := a.lyricsDB().ClearManualLyrics(r.Context(), id)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no_manual_lyrics", "this track has no manual lyrics")
		return
	}
	if a.LyricsNudge != nil {
		a.LyricsNudge()
	}
	a.audit(r, "lyrics.manual_cleared", map[string]string{"track_id": id})
	a.writeTrackLyrics(w, r, id, true)
}

// requeueLyricsRequest is the optional body of POST /tracks/{id}/lyrics/requeue.
type requeueLyricsRequest struct {
	ReplaceManual bool `json:"replace_manual"`
}

// decodeRequeue reads the optional re-queue body (empty = defaults).
func decodeRequeue(r *http.Request) (requeueLyricsRequest, error) {
	var req requeueLyricsRequest
	if r.Body == nil {
		return req, nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return req, err
	}
	return req, nil
}

// requeueLyrics serves POST /tracks/{id}/lyrics/requeue: the track's lyrics
// lookup goes to the front of the queue, whatever its status (found lyrics
// are fetched again; when they changed, the translation is redone). 409
// lyrics_disabled while the lyrics fetcher is off, and 409 manual_lyrics for
// manual lyrics unless the body sets replace_manual: true (the next LRCLIB
// result then replaces them).
func (a *API) requeueLyrics(w http.ResponseWriter, r *http.Request) {
	if !a.LyricsLookup {
		writeError(w, http.StatusConflict, "lyrics_disabled", "the lyrics fetcher is off (LYRICS_ENABLED=false)")
		return
	}
	req, err := decodeRequeue(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be {replace_manual?: boolean}")
		return
	}
	id := chi.URLParam(r, "id")
	ok, err := a.lyricsDB().RequeueLyrics(r.Context(), id, req.ReplaceManual)
	if errors.Is(err, store.ErrManualLyrics) {
		writeError(w, http.StatusConflict, "manual_lyrics",
			"this track has lyrics added by hand; set replace_manual: true to replace them with LRCLIB's")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "track not found, or it has no title or artist to look up")
		return
	}
	if a.LyricsNudge != nil {
		a.LyricsNudge()
	}
	detail := map[string]any{"track_id": id}
	if req.ReplaceManual {
		detail["replace_manual"] = true
	}
	a.audit(r, "lyrics.requeued", detail)
	writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}
