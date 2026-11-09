package topplaylists

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

type key struct {
	period string
	start  int64
}

type fakeStore struct {
	rows      map[key]domain.TopPlaylistRecord
	snapshots map[key][]domain.RankedTrack
	ranked    []domain.RankedTrack
	rankErr   error
	rankEnd   time.Time // end of the last ranking window
	snapErr   error
	upsertErr func(p domain.TopPlaylistRecord) error // injected upsert failure
	upserts   int
	locked    bool // another instance holds the lock
	unlocks   int
	ext       storeExt // prune / verify state (prune_test.go)
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[key]domain.TopPlaylistRecord{}, snapshots: map[key][]domain.RankedTrack{}}
}

func (f *fakeStore) TopPlaylist(_ context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error) {
	r, ok := f.rows[key{period, start.Unix()}]
	if !ok {
		return r, domain.ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) LockTopPlaylists(context.Context) (func(), bool, error) {
	if f.locked {
		return nil, false, nil
	}
	return func() { f.unlocks++ }, true, nil
}

func (f *fakeStore) UpsertTopPlaylist(ctx context.Context, p domain.TopPlaylistRecord) error {
	f.upserts++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if f.upsertErr != nil {
		if err := f.upsertErr(p); err != nil {
			return err
		}
	}
	k := key{p.Period, p.PeriodStart.Unix()}
	if old, ok := f.rows[k]; ok && old.Status == domain.TopPlaylistPruned {
		return nil // mirrors the WHERE in SQL: a pruned row never changes
	}
	if old, ok := f.rows[k]; ok && p.SpotifyPlaylistID == "" { // mirrors the COALESCE in SQL
		p.SpotifyPlaylistID, p.SpotifyURL = old.SpotifyPlaylistID, old.SpotifyURL
	}
	f.rows[k] = p
	return nil
}

func (f *fakeStore) SaveTopPlaylistSnapshot(ctx context.Context, p domain.TopPlaylistRecord, tracks []domain.RankedTrack) error {
	f.snapshots[key{p.Period, p.PeriodStart.Unix()}] = tracks
	return f.UpsertTopPlaylist(ctx, p)
}

func (f *fakeStore) RankTracks(_ context.Context, _, end time.Time, limit int) ([]domain.RankedTrack, error) {
	if limit != TrackLimit {
		return nil, fmt.Errorf("limit %d", limit)
	}
	f.rankEnd = end
	return f.ranked, f.rankErr
}

func (f *fakeStore) EndedLiveTopPlaylists(_ context.Context, endedBy, now time.Time, limit int) ([]domain.TopPlaylistRecord, error) {
	var out []domain.TopPlaylistRecord
	for _, r := range f.rows {
		backingOff := r.Attempts > 0 && r.NextAttemptAt != nil && r.NextAttemptAt.After(now)
		if r.Status == domain.TopPlaylistCreated && r.Live && !r.PeriodEnd.After(endedBy) && !backingOff {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PeriodStart.Before(out[j].PeriodStart) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) TopPlaylistSnapshot(_ context.Context, period string, start time.Time) ([]domain.RankedTrack, error) {
	return f.snapshots[key{period, start.Unix()}], f.snapErr
}

type fakeSpotify struct {
	onCreate  func() // runs after a successful create (e.g. to cancel the tick)
	creates   []string
	replaces  map[string][]string
	createErr error
	replErr   error
	descrs    []string          // create descriptions, in order
	details   map[string]string // id -> "name|description" of the last details update
	detErr    error
	ext       spotifyExt // follow / unfollow / library / profile (prune_test.go)
}

func (f *fakeSpotify) CreatePlaylist(ctx context.Context, name, descr string, public bool) (string, string, error) {
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if f.createErr != nil {
		return "", "", f.createErr
	}
	if !public {
		return "", "", errors.New("want public")
	}
	f.creates = append(f.creates, name)
	f.descrs = append(f.descrs, descr)
	id := fmt.Sprintf("pl%d", len(f.creates))
	if f.onCreate != nil {
		f.onCreate()
	}
	return id, "https://open.spotify.com/playlist/" + id, nil
}

func (f *fakeSpotify) ReplacePlaylistItems(_ context.Context, id string, uris []string) error {
	if f.ext.replErrFor != nil {
		if err := f.ext.replErrFor(id); err != nil {
			return err
		}
	}
	if f.replErr != nil {
		return f.replErr
	}
	if f.replaces == nil {
		f.replaces = map[string][]string{}
	}
	f.replaces[id] = uris
	return nil
}

func (f *fakeSpotify) UpdatePlaylistDetails(_ context.Context, id, name, description string) error {
	f.ext.detailCalls++
	if f.ext.detErrFor != nil {
		if err := f.ext.detErrFor(id); err != nil {
			return err
		}
	}
	if f.detErr != nil {
		return f.detErr
	}
	if f.details == nil {
		f.details = map[string]string{}
	}
	f.details[id] = name + "|" + description
	return nil
}

type fakeAuth struct {
	s   settings.Spotify
	err error
}

func (f *fakeAuth) Spotify(context.Context) (settings.Spotify, error) { return f.s, f.err }

type harness struct {
	r     *Runner
	st    *fakeStore
	sp    *fakeSpotify
	auth  *fakeAuth
	logs  *bytes.Buffer
	now   time.Time
	inval int
}

func newHarness(t *testing.T, periods ...Period) *harness {
	t.Helper()
	h := &harness{st: newFakeStore(), sp: &fakeSpotify{}, logs: &bytes.Buffer{},
		auth: &fakeAuth{s: settings.Spotify{RefreshToken: "rt", Scopes: []string{spotify.RequiredScope, spotify.PlaylistWriteScope}}},
		now:  time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)}
	log := slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.r = New(Config{Periods: periods, Location: time.UTC}, h.st, h.sp, h.auth,
		func(context.Context) { h.inval++ }, log)
	h.r.now = func() time.Time { return h.now }
	return h
}

var weekStart = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

func (h *harness) week() domain.TopPlaylistRecord {
	return h.st.rows[key{"week", weekStart.Unix()}]
}

func ranked(n int) []domain.RankedTrack {
	out := make([]domain.RankedTrack, n)
	for i := range out {
		out[i] = domain.RankedTrack{TrackID: fmt.Sprintf("t%d", i+1), Plays: int64(n - i)}
	}
	return out
}

func TestRunnerCreatesPlaylist(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.r.RunOnce(context.Background())

	w := h.week()
	if w.Status != domain.TopPlaylistCreated || w.SpotifyPlaylistID != "pl1" || w.TrackCount != 3 || w.TotalPlays != 6 ||
		w.PublishedAt == nil || w.Attempts != 0 || w.Name != "Top 50 · Week of Nov 2, 2026" || w.TimeZone != "UTC" ||
		!w.PeriodEnd.Equal(weekStart.AddDate(0, 0, 7)) {
		t.Fatalf("row = %+v", w)
	}
	if got := h.sp.replaces["pl1"]; len(got) != 3 || got[0] != "spotify:track:t1" {
		t.Errorf("uris = %v", got)
	}
	if len(h.st.snapshots[key{"week", weekStart.Unix()}]) != 3 || h.inval != 1 {
		t.Errorf("snapshot/invalidate not done (inval=%d)", h.inval)
	}
	if !strings.Contains(h.logs.String(), "top playlist published") {
		t.Errorf("missing info log: %s", h.logs)
	}

	// Done: the next tick does nothing.
	upserts := h.st.upserts
	h.r.RunOnce(context.Background())
	if h.st.upserts != upserts || len(h.sp.creates) != 1 {
		t.Error("created period was processed again")
	}
}

func TestRunnerAllKinds(t *testing.T) {
	h := newHarness(t, Week, Month, Year)
	h.st.ranked = ranked(2)
	h.r.RunOnce(context.Background())
	want := []string{"Top 50 · Week of Nov 2, 2026", "Top 50 · October 2026", "Top 50 · 2025"}
	if strings.Join(h.sp.creates, "|") != strings.Join(want, "|") {
		t.Errorf("creates = %v", h.sp.creates)
	}
}

func TestRunnerEmpty(t *testing.T) {
	h := newHarness(t, Week)
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistEmpty || len(h.sp.creates) != 0 {
		t.Fatalf("row = %+v", w)
	}
	h.st.ranked = ranked(1) // late data does not revive an empty period
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 0 {
		t.Error("empty period was retried")
	}
}

func TestRunnerWaitingAuthLogsOnce(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(1)
	h.auth.s.Scopes = []string{spotify.RequiredScope}
	for range 3 {
		h.r.RunOnce(context.Background())
	}
	w := h.week()
	if w.Status != domain.TopPlaylistWaitingAuth || !strings.Contains(w.LastError, "playlist-modify-public") {
		t.Fatalf("row = %+v", w)
	}
	if n := strings.Count(h.logs.String(), "waiting for spotify authorization"); n != 1 {
		t.Errorf("warned %d times, want once", n)
	}
	if h.st.upserts != 1 || len(h.sp.creates) != 0 {
		t.Errorf("upserts=%d creates=%d", h.st.upserts, len(h.sp.creates))
	}

	// After re-authorizing, the next tick publishes.
	h.auth.s.Scopes = append(h.auth.s.Scopes, spotify.PlaylistWriteScope)
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistCreated || w.LastError != "" {
		t.Fatalf("row = %+v", w)
	}
}

func TestRunnerNotConnected(t *testing.T) {
	h := newHarness(t, Week)
	h.auth.err = settings.ErrNotFound
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistWaitingAuth || !strings.HasPrefix(w.LastError, "connect Spotify") {
		t.Fatalf("row = %+v", w)
	}
}

func TestRunnerForbiddenWaitsForAuth(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(1)
	h.sp.createErr = fmt.Errorf("%w: create playlist: scope", spotify.ErrForbidden)
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistWaitingAuth || w.Attempts != 0 {
		t.Fatalf("row = %+v", w)
	}
}

func TestRunnerRetryReusesPlaylistID(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.sp.replErr = errors.New("spotify: replace playlist items failed (502): bad gateway")
	h.r.RunOnce(context.Background())

	w := h.week()
	if w.Status != domain.TopPlaylistFailed || w.Attempts != 1 || w.SpotifyPlaylistID != "pl1" ||
		w.NextAttemptAt == nil || !w.NextAttemptAt.Equal(h.now.Add(5*time.Minute)) || !strings.Contains(w.LastError, "502") {
		t.Fatalf("row after failure = %+v", w)
	}

	// Before next_attempt_at: skipped.
	h.sp.replErr = nil
	h.now = h.now.Add(4 * time.Minute)
	h.r.RunOnce(context.Background())
	if h.week().Status != domain.TopPlaylistFailed {
		t.Fatal("retried before next_attempt_at")
	}

	// Due: retried with the same playlist, no second create.
	h.now = h.now.Add(2 * time.Minute)
	h.r.RunOnce(context.Background())
	w = h.week()
	if w.Status != domain.TopPlaylistCreated || w.Attempts != 0 || len(h.sp.creates) != 1 || h.sp.replaces["pl1"] == nil {
		t.Fatalf("row after retry = %+v creates=%v", w, h.sp.creates)
	}
}

func TestRunnerBackoffGrows(t *testing.T) {
	h := newHarness(t, Week)
	h.st.rankErr = errors.New("db down")
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute}
	for i, d := range want {
		h.r.RunOnce(context.Background())
		w := h.week()
		if w.Attempts != i+1 || !w.NextAttemptAt.Equal(h.now.Add(d)) {
			t.Fatalf("attempt %d: %+v", i+1, w)
		}
		h.now = *w.NextAttemptAt
	}
}

func TestRunnerHonoursRetryAfter(t *testing.T) {
	h := newHarness(t, Week, Month)
	h.st.ranked = ranked(1)
	h.sp.createErr = &spotify.RateLimitError{RetryAfter: time.Hour}
	h.r.RunOnce(context.Background())
	w := h.week()
	if w.Status != domain.TopPlaylistFailed || !w.NextAttemptAt.Equal(h.now.Add(time.Hour)) {
		t.Fatalf("row = %+v", w)
	}
	if _, ok := h.st.rows[key{"month", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix()}]; ok {
		t.Error("month processed after a rate limit")
	}
}

func TestTruncateError(t *testing.T) {
	long := strings.Repeat("é", 400)
	if got := []rune(truncateError(long)); len(got) != maxErrorLen {
		t.Errorf("len = %d", len(got))
	}
	if truncateError("short") != "short" {
		t.Error("short message changed")
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	h := newHarness(t, Week)
	var waits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	h.r.sleep = func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		if len(waits) == 2 {
			cancel()
			return false
		}
		return true
	}
	if err := h.r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(waits) != 2 || waits[0] != time.Minute || waits[1] != 10*time.Minute {
		t.Errorf("waits = %v", waits)
	}
	if h.week().Status != domain.TopPlaylistEmpty {
		t.Error("one tick expected between the waits")
	}
}

func TestRunnerSkipsWhenLockHeld(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.st.locked = true
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 0 || h.st.upserts != 0 {
		t.Fatalf("worked without the lock: creates=%d upserts=%d", len(h.sp.creates), h.st.upserts)
	}
	h.st.locked = false
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 1 || h.st.unlocks != 1 {
		t.Fatalf("creates=%d unlocks=%d, want 1 and 1", len(h.sp.creates), h.st.unlocks)
	}
}

// A shutdown right after Spotify created the playlist must still record its
// ID, so the next run reuses it instead of creating a duplicate.
func TestRunnerCreateSurvivesShutdown(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	ctx, cancel := context.WithCancel(context.Background())
	h.sp.onCreate = cancel
	h.r.RunOnce(ctx)
	if got := h.week().SpotifyPlaylistID; got != "pl1" {
		t.Fatalf("playlist id after shutdown = %q, want pl1", got)
	}
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 1 || h.week().Status != domain.TopPlaylistCreated {
		t.Fatalf("creates=%d status=%s, want 1 create and created", len(h.sp.creates), h.week().Status)
	}
}

func TestRunnerForbiddenBacksOff(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.sp.createErr = fmt.Errorf("create: %w", spotify.ErrForbidden)
	h.r.RunOnce(context.Background())
	w := h.week()
	if w.Status != domain.TopPlaylistWaitingAuth || w.NextAttemptAt == nil || !w.NextAttemptAt.Equal(h.now.Add(time.Hour)) {
		t.Fatalf("after 403: status=%s next=%v", w.Status, w.NextAttemptAt)
	}
	h.sp.createErr = nil
	h.now = h.now.Add(30 * time.Minute)
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 0 {
		t.Fatal("retried before the 403 back-off elapsed")
	}
	h.now = h.now.Add(31 * time.Minute)
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 1 || h.week().Status != domain.TopPlaylistCreated {
		t.Fatalf("creates=%d status=%s after back-off", len(h.sp.creates), h.week().Status)
	}
}

func TestRunnerRevokedToken(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.sp.createErr = spotify.ErrInvalidGrant
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistWaitingAuth || w.LastError != msgRevoked {
		t.Fatalf("status=%s err=%q", w.Status, w.LastError)
	}
}
