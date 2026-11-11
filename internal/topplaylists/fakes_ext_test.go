package topplaylists

import (
	"context"
	"sort"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// storeExt is the prune / verify state of fakeStore.
type storeExt struct {
	createdErr error
	verified   int // MarkVerified calls
}

func (f *fakeStore) created() []domain.TopPlaylistRecord {
	var out []domain.TopPlaylistRecord
	for _, r := range f.rows {
		if r.Status == domain.TopPlaylistCreated {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeStore) CreatedTopPlaylists(_ context.Context, period string) ([]domain.TopPlaylistRecord, error) {
	var out []domain.TopPlaylistRecord
	for _, r := range f.created() {
		if r.Period == period {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodStart.After(out[j].PeriodStart) })
	return out, f.ext.createdErr
}

func (f *fakeStore) AllCreatedTopPlaylists(context.Context) ([]domain.TopPlaylistRecord, error) {
	out := f.created()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].PeriodStart.Equal(out[j].PeriodStart) {
			return out[i].PeriodStart.Before(out[j].PeriodStart)
		}
		return out[i].Period < out[j].Period
	})
	return out, f.ext.createdErr
}

func (f *fakeStore) PruneTopPlaylist(_ context.Context, period string, start, now time.Time) error {
	k := key{period, start.Unix()}
	r, ok := f.rows[k]
	if !ok {
		return domain.ErrNotFound
	}
	r.Status, r.PrunedAt, r.SpotifyRemoved, r.Live = domain.TopPlaylistPruned, &now, false, false
	r.NextAttemptAt, r.Attempts = nil, 0
	f.rows[k] = r
	delete(f.snapshots, k)
	return nil
}

func (f *fakeStore) PendingUnfollows(_ context.Context, limit int) ([]domain.TopPlaylistRecord, error) {
	var out []domain.TopPlaylistRecord
	for _, r := range f.rows {
		if r.Status == domain.TopPlaylistPruned && !r.SpotifyRemoved && r.SpotifyPlaylistID != "" {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodStart.Before(out[j].PeriodStart) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) MarkSpotifyRemoved(_ context.Context, period string, start time.Time) error {
	k := key{period, start.Unix()}
	if r, ok := f.rows[k]; ok && r.Status == domain.TopPlaylistPruned {
		r.SpotifyRemoved, r.LastError = true, ""
		f.rows[k] = r
	}
	return nil
}

func (f *fakeStore) SetTopPlaylistError(_ context.Context, period string, start time.Time, msg string) error {
	k := key{period, start.Unix()}
	if r, ok := f.rows[k]; ok {
		r.LastError = msg
		f.rows[k] = r
	}
	return nil
}

func (f *fakeStore) MarkVerified(_ context.Context, keys []domain.TopPlaylistKey, now time.Time) error {
	f.ext.verified++
	for _, k := range keys {
		kk := key{k.Period, k.Start.Unix()}
		if r, ok := f.rows[kk]; ok {
			r.VerifiedAt = &now
			f.rows[kk] = r
		}
	}
	return nil
}

func (f *fakeStore) LastTopPlaylistVerify(context.Context) (*time.Time, error) {
	var last *time.Time
	for _, r := range f.created() {
		if r.VerifiedAt != nil && (last == nil || r.VerifiedAt.After(*last)) {
			last = r.VerifiedAt
		}
	}
	return last, nil
}

// spotifyExt is the follow / unfollow / library / profile state of fakeSpotify.
type spotifyExt struct {
	unfollows   []string
	unfollowErr error
	onUnfollow  func(id string)
	follows     []string
	followErr   error
	owned       map[string]bool
	ownedErr    error
	ownedCalls  int
	userName    string
	userErr     error
	userCalls   int
	detailCalls int
	detErrFor   func(id string) error
	replErrFor  func(id string) error
	named       map[string]string // name -> id of every created playlist
	loseCreates int               // creates whose response is lost (error after creating)
	findCalls   int
	findErr     error
}

func (f *fakeSpotify) UnfollowPlaylist(_ context.Context, id string) error {
	if f.ext.onUnfollow != nil {
		f.ext.onUnfollow(id)
	}
	if f.ext.unfollowErr != nil {
		return f.ext.unfollowErr
	}
	f.ext.unfollows = append(f.ext.unfollows, id)
	return nil
}

func (f *fakeSpotify) FollowPlaylist(_ context.Context, id string) error {
	if f.ext.followErr != nil {
		return f.ext.followErr
	}
	f.ext.follows = append(f.ext.follows, id)
	return nil
}

func (f *fakeSpotify) OwnedPlaylistIDs(context.Context) (map[string]bool, error) {
	f.ext.ownedCalls++
	return f.ext.owned, f.ext.ownedErr
}

func (f *fakeSpotify) FindOwnedPlaylist(_ context.Context, name string) (string, string, error) {
	f.ext.findCalls++
	if f.ext.findErr != nil {
		return "", "", f.ext.findErr
	}
	id, ok := f.ext.named[name]
	if !ok {
		return "", "", nil
	}
	return id, "https://open.spotify.com/playlist/" + id, nil
}

func (f *fakeSpotify) CurrentUserName(context.Context) (string, error) {
	f.ext.userCalls++
	return f.ext.userName, f.ext.userErr
}

// addCreated stores a created row of kind p starting at start, with a
// snapshot of n tracks and Spotify ID id.
func (h *harness) addCreated(p Period, start time.Time, id string, n int) domain.TopPlaylistRecord {
	_, end := Bounds(p, start, time.UTC)
	pub := start
	rec := domain.TopPlaylistRecord{Period: string(p), PeriodStart: start, PeriodEnd: end, TimeZone: "UTC",
		Name: Name(p, start), Status: domain.TopPlaylistCreated, SpotifyPlaylistID: id,
		SpotifyURL: "https://open.spotify.com/playlist/" + id, TrackCount: n, PublishedAt: &pub}
	k := key{string(p), start.Unix()}
	h.st.rows[k] = rec
	h.st.snapshots[k] = ranked(n)
	return rec
}

func (h *harness) row(p Period, start time.Time) domain.TopPlaylistRecord {
	return h.st.rows[key{string(p), start.Unix()}]
}
