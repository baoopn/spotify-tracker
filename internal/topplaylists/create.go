package topplaylists

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
)

// create makes the Spotify playlist and persists its ID before anything
// else can fail, so a retry reuses it. Both steps run detached from shutdown
// (bounded by createTimeout): a create that reached Spotify is always saved.
// On a save error the returned record carries the new ID and err is set; the
// ID is also kept in memory, so the next attempt of this process saves and
// reuses it instead of creating another playlist.
//
// With lookup set (the row existed before: an earlier attempt failed or
// crashed, so its create may have reached Spotify with the response lost)
// an owned playlist with the same name is adopted instead of creating a new
// one; adopted reports that.
func (r *Runner) create(ctx context.Context, rec domain.TopPlaylistRecord, description string,
	lookup bool) (out domain.TopPlaylistRecord, adopted bool, err error) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), createTimeout)
	defer cancel()
	k := keyOf(rec)
	u, ok := r.unsavedPlaylist(k)
	if !ok && lookup {
		id, link, err := r.Spotify.FindOwnedPlaylist(cctx, rec.Name)
		if err != nil {
			return rec, false, fmt.Errorf("look for an earlier playlist: %w", err)
		}
		if id != "" {
			r.Log.InfoContext(ctx, "adopting spotify playlist from an earlier attempt",
				slog.String("name", rec.Name), slog.String("spotify_id", id))
			u, ok, adopted = unsavedPlaylist{id: id, url: link}, true, true
		}
	}
	if !ok {
		id, link, err := r.Spotify.CreatePlaylist(cctx, rec.Name, description, true)
		if err != nil {
			return rec, false, err
		}
		u = unsavedPlaylist{id: id, url: link}
	}
	rec.SpotifyPlaylistID, rec.SpotifyURL = u.id, u.url
	if err := r.Store.UpsertTopPlaylist(cctx, rec); err != nil {
		r.setUnsaved(k, &u)
		r.Log.ErrorContext(ctx, "save created spotify playlist id failed",
			slog.String("name", rec.Name), slog.String("spotify_id", u.id), logging.Err(err))
		return rec, adopted, err
	}
	r.setUnsaved(k, nil)
	return rec, adopted, nil
}
