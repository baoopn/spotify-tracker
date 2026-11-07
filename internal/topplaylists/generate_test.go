package topplaylists

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

func TestGenerateBackfillsOlderPeriod(t *testing.T) {
	h := newHarness(t, Week, Month)
	h.st.ranked = ranked(4)
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	got, err := h.r.Generate(context.Background(), Month, sep)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.TopPlaylistCreated || got.Name != "Top 50 · September 2026" || got.SpotifyPlaylistID != "pl1" ||
		got.TrackCount != 4 || !got.PeriodEnd.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("record = %+v", got)
	}
	if got != h.st.rows[key{"month", sep.Unix()}] {
		t.Error("returned record is not the stored row")
	}
	if len(h.st.rows) != 1 || h.st.unlocks != 1 || h.inval != 1 {
		t.Errorf("rows=%d unlocks=%d inval=%d", len(h.st.rows), h.st.unlocks, h.inval)
	}
}

func TestGenerateRejectsRunningFutureAndMisaligned(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(1)
	cases := []struct {
		start time.Time
		want  error
	}{
		{time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), ErrNotCompleted},  // running (now is Nov 11)
		{time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC), ErrNotCompleted}, // future
		{time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), ErrMisaligned},    // a Tuesday
		{weekStart.Add(time.Hour), ErrMisaligned},
	}
	for _, c := range cases {
		if _, err := h.r.Generate(context.Background(), Week, c.start); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.start, err, c.want)
		}
	}
	if !errors.Is(ErrMisaligned, ErrNotCompleted) {
		t.Error("ErrMisaligned should match ErrNotCompleted")
	}
	if h.st.upserts != 0 || len(h.sp.creates) != 0 {
		t.Errorf("upserts=%d creates=%d", h.st.upserts, len(h.sp.creates))
	}
}

func TestGenerateDisabledKind(t *testing.T) {
	h := newHarness(t, Week)
	if _, err := h.r.Generate(context.Background(), Month, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrPeriodDisabled) {
		t.Fatalf("err = %v", err)
	}
	off := newHarness(t)
	if _, err := off.r.Generate(context.Background(), Week, weekStart); !errors.Is(err, ErrPeriodDisabled) {
		t.Fatalf("disabled feature: err = %v", err)
	}
}

func TestGenerateAndRunNowBusy(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(1)
	h.st.locked = true
	if _, err := h.r.Generate(context.Background(), Week, weekStart); !errors.Is(err, ErrBusy) {
		t.Errorf("Generate err = %v", err)
	}
	if err := h.r.RunNow(context.Background()); !errors.Is(err, ErrBusy) {
		t.Errorf("RunNow err = %v", err)
	}
	h.r.RunOnce(context.Background()) // silently skipped
	if h.st.upserts != 0 {
		t.Errorf("upserts = %d", h.st.upserts)
	}
	h.st.locked = false
	if err := h.r.RunNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.week().Status != domain.TopPlaylistCreated || h.st.unlocks != 1 {
		t.Errorf("status=%s unlocks=%d", h.week().Status, h.st.unlocks)
	}
}

func TestGenerateRegeneratesCreatedIntoSamePlaylist(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.r.RunOnce(context.Background())
	if h.week().Status != domain.TopPlaylistCreated {
		t.Fatalf("setup: %+v", h.week())
	}

	h.st.ranked = ranked(5)
	h.now = h.now.Add(time.Hour)
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.sp.creates) != 1 {
		t.Fatalf("creates = %v, want no duplicate playlist", h.sp.creates)
	}
	if got.Status != domain.TopPlaylistCreated || got.SpotifyPlaylistID != "pl1" || got.TrackCount != 5 || got.TotalPlays != 15 ||
		got.PublishedAt == nil || !got.PublishedAt.Equal(h.now) {
		t.Fatalf("record = %+v", got)
	}
	if n := len(h.sp.replaces["pl1"]); n != 5 {
		t.Errorf("replaced with %d items, want 5", n)
	}
	if n := len(h.st.snapshots[key{"week", weekStart.Unix()}]); n != 5 {
		t.Errorf("snapshot has %d tracks, want 5", n)
	}
}

func TestGenerateEmptyBecomesCreated(t *testing.T) {
	h := newHarness(t, Week)
	h.r.RunOnce(context.Background())
	if h.week().Status != domain.TopPlaylistEmpty {
		t.Fatalf("setup: %+v", h.week())
	}
	h.st.ranked = ranked(2) // plays imported late
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err != nil || got.Status != domain.TopPlaylistCreated || len(h.sp.creates) != 1 {
		t.Fatalf("err=%v record=%+v creates=%v", err, got, h.sp.creates)
	}
}

func TestGenerateStillEmpty(t *testing.T) {
	h := newHarness(t, Week)
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err != nil || got.Status != domain.TopPlaylistEmpty || len(h.sp.creates) != 0 {
		t.Fatalf("err=%v record=%+v", err, got)
	}
}

func TestGenerateIgnoresBackoff(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.sp.replErr = errors.New("spotify: 502")
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistFailed || !w.NextAttemptAt.After(h.now) {
		t.Fatalf("setup: %+v", w)
	}
	h.sp.replErr = nil
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err != nil || got.Status != domain.TopPlaylistCreated || len(h.sp.creates) != 1 || got.Attempts != 0 {
		t.Fatalf("err=%v record=%+v creates=%v", err, got, h.sp.creates)
	}
}

func TestGenerateForbiddenStoredAndReturned(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.sp.createErr = fmt.Errorf("create playlist: %w", spotify.ErrForbidden)
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if !errors.Is(err, spotify.ErrForbidden) || !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("err = %v", err)
	}
	if got.Status != domain.TopPlaylistWaitingAuth || got.LastError != msgMissingScope || got.NextAttemptAt == nil {
		t.Fatalf("record = %+v", got)
	}
	if got != h.week() {
		t.Error("returned record is not the stored row")
	}
}

func TestGenerateSpotifyFailureStoredAndReturned(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.sp.createErr = errors.New("spotify: create playlist failed (500)")
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err == nil || errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("err = %v", err)
	}
	if got.Status != domain.TopPlaylistFailed || got.Attempts != 1 || got.LastError == "" {
		t.Fatalf("record = %+v", got)
	}
}

func TestGenerateMissingScopeBeforeCalling(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.auth.s.Scopes = []string{spotify.RequiredScope}
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if !errors.Is(err, ErrNotAuthorized) || got.Status != domain.TopPlaylistWaitingAuth || len(h.sp.creates) != 0 {
		t.Fatalf("err=%v record=%+v", err, got)
	}
}

// A row generated before the time zone was changed is regenerated in its
// own zone.
func TestGenerateKeepsRowZone(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(1)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, ny)
	k := key{"week", start.Unix()}
	h.st.rows[k] = domain.TopPlaylistRecord{Period: "week", PeriodStart: start.UTC(), TimeZone: "America/New_York",
		Status: domain.TopPlaylistCreated, SpotifyPlaylistID: "old", SpotifyURL: "https://open.spotify.com/playlist/old"}
	got, err := h.r.Generate(context.Background(), Week, start.UTC())
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeZone != "America/New_York" || got.SpotifyPlaylistID != "old" || len(h.sp.creates) != 0 ||
		got.Name != "Top 50 · Week of Oct 26, 2026" {
		t.Fatalf("record = %+v", got)
	}
}

// published sets up a created week row (3 tracks, published at h.now) and
// returns it.
func (h *harness) published(t *testing.T) domain.TopPlaylistRecord {
	t.Helper()
	h.st.ranked = ranked(3)
	h.r.RunOnce(context.Background())
	w := h.week()
	if w.Status != domain.TopPlaylistCreated {
		t.Fatalf("setup: %+v", w)
	}
	h.now = h.now.Add(time.Hour)
	h.inval = 0
	return w
}

// stillPublished asserts the row is unchanged except for last_error.
func (h *harness) stillPublished(t *testing.T, before domain.TopPlaylistRecord, lastError string) {
	t.Helper()
	w := h.week()
	if w.Status != domain.TopPlaylistCreated || w.PublishedAt == nil || !w.PublishedAt.Equal(*before.PublishedAt) ||
		w.TrackCount != before.TrackCount || w.Attempts != before.Attempts || w.NextAttemptAt != nil ||
		w.LastError != lastError || w.SpotifyPlaylistID != before.SpotifyPlaylistID {
		t.Fatalf("row = %+v, want published row kept with last_error %q", w, lastError)
	}
	if n := len(h.st.snapshots[key{"week", weekStart.Unix()}]); n != before.TrackCount {
		t.Errorf("snapshot has %d tracks, want the old %d", n, before.TrackCount)
	}
	if h.inval != 0 {
		t.Errorf("cache invalidated %d times", h.inval)
	}
	// The tick still skips it.
	upserts := h.st.upserts
	h.r.RunOnce(context.Background())
	if h.st.upserts != upserts {
		t.Error("tick processed the kept row")
	}
}

func TestRegeneratePublishedReplaceFailureKeepsItPublished(t *testing.T) {
	h := newHarness(t, Week)
	before := h.published(t)
	h.st.ranked = ranked(5)
	h.sp.replErr = errors.New("spotify: replace playlist items failed (502): bad gateway")
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err == nil || errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("err = %v", err)
	}
	if got.Status != domain.TopPlaylistCreated || !strings.Contains(got.LastError, "502") {
		t.Fatalf("returned = %+v", got)
	}
	h.stillPublished(t, before, got.LastError)
	if len(h.sp.creates) != 1 {
		t.Errorf("creates = %v", h.sp.creates)
	}
}

func TestRegeneratePublishedForbiddenKeepsItPublished(t *testing.T) {
	h := newHarness(t, Week)
	before := h.published(t)
	h.sp.replErr = fmt.Errorf("replace: %w", spotify.ErrForbidden)
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if !errors.Is(err, ErrNotAuthorized) || !errors.Is(err, spotify.ErrForbidden) || got.Status != domain.TopPlaylistCreated {
		t.Fatalf("err=%v record=%+v", err, got)
	}
	h.stillPublished(t, before, msgMissingScope)
}

func TestRegeneratePublishedRankFailureKeepsItPublished(t *testing.T) {
	h := newHarness(t, Week)
	before := h.published(t)
	h.st.rankErr = errors.New("db down")
	if _, err := h.r.Generate(context.Background(), Week, weekStart); err == nil {
		t.Fatal("want error")
	}
	h.st.rankErr = nil
	h.stillPublished(t, before, "db down")
}

func TestRegeneratePublishedMissingScopeWritesNothing(t *testing.T) {
	h := newHarness(t, Week)
	before := h.published(t)
	h.auth.s.Scopes = []string{spotify.RequiredScope}
	upserts := h.st.upserts
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if !errors.Is(err, ErrNotAuthorized) || h.st.upserts != upserts || got != before {
		t.Fatalf("err=%v upserts=%d->%d record=%+v", err, upserts, h.st.upserts, got)
	}
}

func TestRegeneratePublishedNoPlaysLeavesItUnchanged(t *testing.T) {
	h := newHarness(t, Week)
	before := h.published(t)
	h.st.ranked = nil
	upserts := h.st.upserts
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if !errors.Is(err, ErrNoPlays) || h.st.upserts != upserts || got != before || len(h.sp.replaces["pl1"]) != 3 {
		t.Fatalf("err=%v upserts=%d->%d record=%+v", err, upserts, h.st.upserts, got)
	}
}

func TestRegeneratePublishedSuccessClearsLastError(t *testing.T) {
	h := newHarness(t, Week)
	h.published(t)
	h.sp.replErr = errors.New("spotify: 500")
	_, _ = h.r.Generate(context.Background(), Week, weekStart)
	if h.week().LastError == "" {
		t.Fatal("setup: last_error not recorded")
	}
	h.sp.replErr = nil
	h.st.ranked = ranked(4)
	got, err := h.r.Generate(context.Background(), Week, weekStart)
	if err != nil || got.LastError != "" || got.TrackCount != 4 || !got.PublishedAt.Equal(h.now) || h.inval != 1 {
		t.Fatalf("err=%v record=%+v inval=%d", err, got, h.inval)
	}
	if n := len(h.st.snapshots[key{"week", weekStart.Unix()}]); n != 4 {
		t.Errorf("snapshot = %d tracks", n)
	}
}

// A request timeout in the middle of a forced generation still records the
// outcome and returns the stored row.
func TestGenerateRecordsOutcomeAfterCancel(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	ctx, cancel := context.WithCancel(context.Background())
	h.sp.onCreate = cancel
	got, err := h.r.Generate(ctx, Week, weekStart)
	if err != nil || got.Status != domain.TopPlaylistCreated || got.SpotifyPlaylistID != "pl1" {
		t.Fatalf("err=%v record=%+v", err, got)
	}
}
