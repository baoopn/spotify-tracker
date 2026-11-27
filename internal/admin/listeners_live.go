package admin

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// ListenPresence reads and clears who is on /listen right now (implemented
// by *listen.PresenceStore).
type ListenPresence interface {
	List(ctx context.Context) ([]listen.Presence, error)
	Delete(ctx context.Context, listenerID int64) error
}

// liveStore looks up listener statuses and track names (implemented by
// *store.Store).
type liveStore interface {
	ListenerStatuses(ctx context.Context, ids []int64) (map[int64]string, error)
	LiveTracks(ctx context.Context, ids []string) (map[string]store.LiveTrack, error)
}

type livePresenceView struct {
	ListenerID int64            `json:"listener_id"`
	Email      string           `json:"email"`
	State      string           `json:"state"`
	Country    *string          `json:"country"`
	Device     string           `json:"device"`
	Browser    string           `json:"browser"`
	Since      time.Time        `json:"since"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Track      *store.LiveTrack `json:"track"`
}

func (a *API) liveDB() liveStore {
	if a.liveLookup != nil {
		return a.liveLookup
	}
	return a.Store
}

var stateRank = map[string]int{listen.StateListening: 0, listen.StateFollowing: 1, listen.StateIdle: 2}

// listenersLive serves GET /listeners/live: who is on /listen right now,
// listening first, then following, then idle, each longest-present first.
// Revoked (or deleted) listeners are left out.
func (a *API) listenersLive(w http.ResponseWriter, r *http.Request) {
	out := []livePresenceView{}
	if a.Presence == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
		return
	}
	ctx := r.Context()
	ps, err := a.Presence.List(ctx)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	ids := make([]int64, 0, len(ps))
	var tracks []string
	for _, p := range ps {
		ids = append(ids, p.ListenerID)
		if p.TrackID != nil && !slices.Contains(tracks, *p.TrackID) {
			tracks = append(tracks, *p.TrackID)
		}
	}
	db := a.liveDB()
	statuses, err := db.ListenerStatuses(ctx, ids)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	names, err := db.LiveTracks(ctx, tracks)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	for _, p := range ps {
		if st, ok := statuses[p.ListenerID]; !ok || st == store.ListenerRevoked {
			continue
		}
		v := livePresenceView{ListenerID: p.ListenerID, Email: p.Email, State: p.State, Device: p.Device,
			Browser: p.Browser, Since: p.Since.UTC(), UpdatedAt: p.UpdatedAt.UTC()}
		if p.Country != "" {
			c := p.Country
			v.Country = &c
		}
		if p.TrackID != nil {
			t, ok := names[*p.TrackID]
			if !ok {
				t = store.LiveTrack{ID: *p.TrackID, Artists: []store.ListenPlayArtist{}}
			}
			v.Track = &t
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(x, y livePresenceView) int {
		if d := stateRank[x.State] - stateRank[y.State]; d != 0 {
			return d
		}
		if c := x.Since.Compare(y.Since); c != 0 {
			return c
		}
		return int(x.ListenerID - y.ListenerID)
	})
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// dropPresence removes a revoked listener from "Listening now".
func (a *API) dropPresence(ctx context.Context, id int64) {
	if a.Presence == nil {
		return
	}
	if err := a.Presence.Delete(ctx, id); err != nil {
		a.Log.WarnContext(ctx, "removing listener presence failed", logging.Err(err))
	}
}
