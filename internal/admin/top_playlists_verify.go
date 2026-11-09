package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// writePruned answers 409 pruned: the row is history only.
func writePruned(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "pruned",
		"this playlist was pruned: only the newest playlists per kind are kept")
}

// POST /admin/api/top-playlists/verify — check the published playlists
// against Spotify now and restore the ones deleted outside this system.
func (a *API) verifyTopPlaylists(w http.ResponseWriter, r *http.Request) {
	if !a.requireTopPlaylists(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), topPlaylistsTimeout)
	defer cancel()
	err := a.TopPlaylists.Verify(ctx)
	if errors.Is(err, topplaylists.ErrBusy) {
		writeError(w, http.StatusConflict, "busy", "top playlists are being generated; try again shortly")
		return
	}
	if err != nil {
		a.internal(w, r, err)
		return
	}
	a.audit(r, "top_playlists.verified", map[string]any{})
	a.writeTopPlaylistPage(w, r, "", 50, 0)
}
