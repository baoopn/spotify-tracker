package admin

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// AudioChecker asks the bucket whether a track has an audio file (implemented
// by *listen.Locator; it never signs a URL).
type AudioChecker interface {
	Find(ctx context.Context, trackID string) (ext string, found bool, err error)
}

// audioStatusStore reads recorded audio files (implemented by *store.Store).
type audioStatusStore interface {
	TrackAudioByIDs(ctx context.Context, ids []string) (map[string]store.TrackAudio, error)
}

const maxAudioStatusIDs = 50

var audioStatusID = regexp.MustCompile(`^[0-9A-Za-z]{22}$`)

type audioStatusView struct {
	Available bool    `json:"available"`
	Ext       *string `json:"ext"`
	SizeBytes *int64  `json:"size_bytes"`
}

func (a *API) audioStatusDB() audioStatusStore {
	if a.audioStatusStore != nil {
		return a.audioStatusStore
	}
	return a.Store
}

// audioStatus reports, per track ID, whether it can be listened along: a
// track_audio row (one query), and for the first ID (the current song) with
// no row, the bucket itself (cached HEAD lookups), so a file uploaded since
// the last sync shows at once. With listen along or storage off every track
// is unavailable.
func (a *API) audioStatus(ctx context.Context, ids []string) (map[string]audioStatusView, error) {
	out := make(map[string]audioStatusView, len(ids))
	for _, id := range ids {
		out[id] = audioStatusView{}
	}
	if !a.Listen.Enabled || !a.Listen.AudioConfigured || len(ids) == 0 {
		return out, nil
	}
	rows, err := a.audioStatusDB().TrackAudioByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, r := range rows {
		ext, size := r.Ext, r.SizeBytes
		out[id] = audioStatusView{Available: true, Ext: &ext, SizeBytes: &size}
	}
	if _, ok := rows[ids[0]]; !ok && a.AudioCheck != nil {
		ext, found, err := a.AudioCheck.Find(ctx, ids[0])
		switch {
		case err != nil:
			a.Log.WarnContext(ctx, "audio status: bucket check failed", logging.Err(err))
		case found:
			out[ids[0]] = audioStatusView{Available: true, Ext: &ext}
		}
	}
	return out, nil
}

// GET /admin/api/listen/audio/status?ids=<id>,<id>… — whether each track (1
// to 50 Spotify IDs; the first is the current song) has an audio file.
func (a *API) audioStatusRoute(w http.ResponseWriter, r *http.Request) {
	var ids []string
	seen := map[string]bool{}
	for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !audioStatusID.MatchString(id) {
			writeError(w, http.StatusBadRequest, "invalid_ids", "ids must be 1 to 50 Spotify track IDs, comma-separated")
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > maxAudioStatusIDs {
		writeError(w, http.StatusBadRequest, "invalid_ids", "ids must be 1 to 50 Spotify track IDs, comma-separated")
		return
	}
	status, err := a.audioStatus(r.Context(), ids)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": status, "listen_enabled": a.Listen.Enabled,
		"audio_configured": a.Listen.AudioConfigured})
}

type overviewAudioView struct {
	Available bool `json:"available"`
}

// overviewAudio is the overview's listen_audio for the track playing now:
// nil when nothing plays or listen along is off, and nil (logged) on an
// error, so it never fails the overview.
func (a *API) overviewAudio(ctx context.Context, np *domain.NowPlaying) *overviewAudioView {
	if !a.Listen.Enabled || np == nil || np.Track == nil || np.Track.ID == "" {
		return nil
	}
	if !audioStatusID.MatchString(np.Track.ID) {
		return &overviewAudioView{}
	}
	s, err := a.audioStatus(ctx, []string{np.Track.ID})
	if err != nil {
		a.Log.WarnContext(ctx, "overview: audio status failed", logging.Err(err))
		return nil
	}
	return &overviewAudioView{Available: s[np.Track.ID].Available}
}
