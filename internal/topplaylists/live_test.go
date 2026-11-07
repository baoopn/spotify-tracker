package topplaylists

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// The harness clock (Wed Nov 11, 2026) is in the week of Nov 9.
var liveStart = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

// newLiveHarness enables a live week. The last completed week is seeded as
// empty so a tick only touches the live period.
func newLiveHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, Week)
	h.r.Config.Live = []Period{Week}
	h.st.rows[key{"week", weekStart.Unix()}] = domain.TopPlaylistRecord{Period: "week", PeriodStart: weekStart,
		PeriodEnd: liveStart, TimeZone: "UTC", Status: domain.TopPlaylistEmpty}
	return h
}

func (h *harness) live() domain.TopPlaylistRecord {
	return h.st.rows[key{"week", liveStart.Unix()}]
}

func (h *harness) liveSnapshot() []domain.RankedTrack {
	return h.st.snapshots[key{"week", liveStart.Unix()}]
}

// createLive publishes the live week with ranked(3) as pl1.
func createLive(t *testing.T, h *harness) {
	t.Helper()
	h.st.ranked = ranked(3)
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Status != domain.TopPlaylistCreated || !w.Live {
		t.Fatalf("setup: live row = %+v", w)
	}
}

func TestLiveCreatedOnFirstPlay(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	w := h.live()
	if w.SpotifyPlaylistID != "pl1" || w.Name != "Top 50 · Week of Nov 9, 2026" || w.TrackCount != 3 || w.TotalPlays != 6 ||
		w.PublishedAt == nil || !w.PublishedAt.Equal(h.now) || w.RefreshedAt == nil || !w.RefreshedAt.Equal(h.now) ||
		!w.PeriodEnd.Equal(liveStart.AddDate(0, 0, 7)) || w.LastError != "" || w.Attempts != 0 {
		t.Fatalf("row = %+v", w)
	}
	if !h.st.rankEnd.Equal(h.now) {
		t.Errorf("ranked until %s, want now", h.st.rankEnd)
	}
	want := "The 50 most-played tracks of the week of Nov 9, 2026 so far, updated hourly from listening history."
	if len(h.sp.descrs) != 1 || h.sp.descrs[0] != want {
		t.Errorf("descriptions = %q", h.sp.descrs)
	}
	if len(h.sp.replaces["pl1"]) != 3 || len(h.liveSnapshot()) != 3 || h.inval != 1 {
		t.Errorf("replace/snapshot/invalidate missing (inval=%d)", h.inval)
	}
}

func TestLiveNoPlaysNoRow(t *testing.T) {
	h := newLiveHarness(t)
	h.r.RunOnce(context.Background())
	if _, ok := h.st.rows[key{"week", liveStart.Unix()}]; ok || len(h.sp.creates) != 0 || h.st.upserts != 0 {
		t.Fatalf("live row written without plays: rows=%v creates=%v", h.st.rows, h.sp.creates)
	}
}

func TestLiveRefreshSkippedWithinInterval(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	upserts := h.st.upserts
	h.st.ranked = ranked(5)
	h.now = h.now.Add(59 * time.Minute)
	h.r.RunOnce(context.Background())
	if h.st.upserts != upserts || len(h.sp.replaces["pl1"]) != 3 {
		t.Fatalf("refreshed before the interval: upserts %d -> %d", upserts, h.st.upserts)
	}
	h.r.Config.LiveRefresh = 30 * time.Minute // a shorter interval is honoured
	h.r.RunOnce(context.Background())
	if len(h.sp.replaces["pl1"]) != 5 {
		t.Fatal("not refreshed after the configured interval")
	}
}

func TestLiveUnchangedOrderUpdatesPlaysOnly(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	h.sp.replaces = nil
	h.st.ranked = ranked(3)
	for i := range h.st.ranked {
		h.st.ranked[i].Plays += 10
	}
	h.now = h.now.Add(time.Hour)
	h.r.RunOnce(context.Background())
	if h.sp.replaces != nil || len(h.sp.creates) != 1 {
		t.Fatalf("spotify written for an unchanged order: %v", h.sp.replaces)
	}
	w := h.live()
	if w.TotalPlays != 36 || !w.RefreshedAt.Equal(h.now) || w.PublishedAt.Equal(h.now) || !w.Live {
		t.Fatalf("row = %+v", w)
	}
	if snap := h.liveSnapshot(); snap[0].Plays != 13 || h.inval != 2 {
		t.Errorf("snapshot plays = %d, inval = %d", snap[0].Plays, h.inval)
	}

	// Same order and plays: refreshed_at moves, the cache is kept.
	h.now = h.now.Add(time.Hour)
	h.r.RunOnce(context.Background())
	if h.inval != 2 || !h.live().RefreshedAt.Equal(h.now) {
		t.Errorf("inval = %d refreshed = %v", h.inval, h.live().RefreshedAt)
	}
}

func TestLiveChangedOrderReplacesItems(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	rec := h.live() // a previous failure's last_error is cleared by a successful refresh
	rec.LastError = "old"
	h.st.rows[key{"week", liveStart.Unix()}] = rec
	h.st.ranked = []domain.RankedTrack{{TrackID: "t2", Plays: 9}, {TrackID: "t1", Plays: 8}, {TrackID: "t9", Plays: 1}}
	h.now = h.now.Add(time.Hour)
	h.r.RunOnce(context.Background())
	if got := h.sp.replaces["pl1"]; len(got) != 3 || got[0] != "spotify:track:t2" || got[2] != "spotify:track:t9" {
		t.Fatalf("uris = %v", got)
	}
	w := h.live()
	if w.TotalPlays != 18 || w.LastError != "" || !w.RefreshedAt.Equal(h.now) || len(h.sp.creates) != 1 {
		t.Fatalf("row = %+v", w)
	}
	if snap := h.liveSnapshot(); snap[0].TrackID != "t2" || h.inval != 2 {
		t.Errorf("snapshot = %v inval = %d", snap, h.inval)
	}
}

func TestLiveRefreshFailureKeepsPublished(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	published := *h.live().PublishedAt
	h.st.ranked = ranked(5)
	h.sp.replErr = errors.New("spotify: replace playlist items failed (502): bad gateway")
	h.now = h.now.Add(time.Hour)
	h.r.RunOnce(context.Background())
	w := h.live()
	if w.Status != domain.TopPlaylistCreated || !w.Live || !w.PublishedAt.Equal(published) || w.LastError == "" ||
		!w.RefreshedAt.Equal(h.now) || w.NextAttemptAt != nil || w.Attempts != 0 || w.TrackCount != 3 {
		t.Fatalf("row = %+v", w)
	}
	if len(h.liveSnapshot()) != 3 {
		t.Error("old snapshot replaced after a failed refresh")
	}

	// Auth problems keep it published too, with the re-authorize message.
	h.sp.replErr = nil
	h.auth.s.Scopes = nil
	h.auth.s.RefreshToken = ""
	h.now = h.now.Add(time.Hour)
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Status != domain.TopPlaylistCreated || !w.Live || w.LastError != msgNotConnected {
		t.Fatalf("row after auth problem = %+v", w)
	}
}

func TestLiveFailedCreationRetried(t *testing.T) {
	h := newLiveHarness(t)
	h.st.ranked = ranked(2)
	h.sp.createErr = errors.New("spotify: create playlist failed (500)")
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Status != domain.TopPlaylistFailed || w.Live || w.NextAttemptAt == nil {
		t.Fatalf("row = %+v", w)
	}
	h.sp.createErr = nil
	h.now = h.now.Add(4 * time.Minute)
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 0 {
		t.Fatal("retried before next_attempt_at")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Status != domain.TopPlaylistCreated || !w.Live || len(h.sp.creates) != 1 || !h.st.rankEnd.Equal(h.now) {
		t.Fatalf("row = %+v creates = %v", w, h.sp.creates)
	}
}

func TestLiveFinalizedAtPeriodEnd(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	published := *h.live().PublishedAt
	h.st.ranked = ranked(4)
	h.now = time.Date(2026, 11, 16, 0, 5, 0, 0, time.UTC) // the week of Nov 9 has ended
	h.r.RunOnce(context.Background())

	w := h.live()
	if w.Status != domain.TopPlaylistCreated || w.Live || w.SpotifyPlaylistID != "pl1" || w.TrackCount != 4 ||
		!w.PublishedAt.Equal(published) || !w.RefreshedAt.Equal(h.now) || w.LastError != "" || w.NextAttemptAt != nil {
		t.Fatalf("row = %+v", w)
	}
	if len(h.sp.replaces["pl1"]) != 4 {
		t.Errorf("items not replaced: %v", h.sp.replaces["pl1"])
	}
	want := "Top 50 · Week of Nov 9, 2026|The 50 most-played tracks of the week of Nov 9, 2026, generated automatically from listening history."
	if h.sp.details["pl1"] != want {
		t.Errorf("details = %q", h.sp.details["pl1"])
	}
	// The new week starts its own live playlist; the finished one is never duplicated.
	next := h.st.rows[key{"week", time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC).Unix()}]
	if len(h.sp.creates) != 2 || next.SpotifyPlaylistID != "pl2" || !next.Live {
		t.Fatalf("creates = %v next = %+v", h.sp.creates, next)
	}

	// Finalized: later ticks leave it alone.
	h.sp.details = nil
	h.now = h.now.Add(2 * time.Hour)
	h.r.RunOnce(context.Background())
	if h.sp.details["pl1"] != "" || h.live().Live {
		t.Error("finalized playlist processed again")
	}
}

func TestLiveFinalizeFailureKeepsLiveAndBacksOff(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	h.r.Config.Live = nil // only the finalization is under test
	h.sp.detErr = errors.New("spotify: update playlist details failed (500)")
	h.now = time.Date(2026, 11, 16, 0, 5, 0, 0, time.UTC)
	h.r.RunOnce(context.Background())
	w := h.live()
	if w.Status != domain.TopPlaylistCreated || !w.Live || w.LastError == "" || w.Attempts != 1 ||
		w.NextAttemptAt == nil || !w.NextAttemptAt.Equal(h.now.Add(5*time.Minute)) {
		t.Fatalf("row = %+v", w)
	}

	h.sp.detErr = nil
	h.now = h.now.Add(4 * time.Minute)
	h.r.RunOnce(context.Background())
	if h.sp.details != nil {
		t.Fatal("finalize retried before the backoff")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Live || w.Attempts != 0 || w.NextAttemptAt != nil || w.LastError != "" || h.sp.details["pl1"] == "" {
		t.Fatalf("row after retry = %+v", w)
	}
	if end := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC); !h.st.rankEnd.Equal(end) {
		t.Errorf("finalize ranked until %s, want the period end", h.st.rankEnd)
	}
}

// A live playlist created whose first items write failed still has the live
// description: the final attempt fixes it.
func TestFinalAttemptFixesLiveDescription(t *testing.T) {
	h := newLiveHarness(t)
	h.st.ranked = ranked(2)
	h.sp.replErr = errors.New("spotify: replace playlist items failed (502)")
	h.r.RunOnce(context.Background())
	if w := h.live(); w.Status != domain.TopPlaylistFailed || w.SpotifyPlaylistID != "pl1" {
		t.Fatalf("setup = %+v", w)
	}
	h.sp.replErr = nil
	h.r.Config.Live = nil
	h.now = time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	h.r.RunOnce(context.Background())
	w := h.live()
	if w.Status != domain.TopPlaylistCreated || w.Live || len(h.sp.creates) != 1 || h.sp.details["pl1"] == "" {
		t.Fatalf("row = %+v details = %v", w, h.sp.details)
	}
}

func TestRegenerateLiveForcesRefresh(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	h.st.ranked = ranked(5)
	h.now = h.now.Add(5 * time.Minute) // well within the refresh interval
	got, err := h.r.Regenerate(context.Background(), Week, liveStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.sp.replaces["pl1"]) != 5 || got.TrackCount != 5 || !got.RefreshedAt.Equal(h.now) || !got.Live || len(h.sp.creates) != 1 {
		t.Fatalf("record = %+v", got)
	}
	if h.st.unlocks != 2 {
		t.Errorf("unlocks = %d", h.st.unlocks)
	}
	// Generate (the backfill endpoint) still refuses the running period.
	if _, err := h.r.Generate(context.Background(), Week, liveStart); !errors.Is(err, ErrNotCompleted) {
		t.Errorf("Generate err = %v", err)
	}
	// A running period of a kind without live playlists is refused too.
	h.r.Config.Live = nil
	if _, err := h.r.Regenerate(context.Background(), Week, liveStart); !errors.Is(err, ErrNotCompleted) {
		t.Errorf("Regenerate (not live) err = %v", err)
	}
}

func TestRegenerateCompletedLiveFinalizes(t *testing.T) {
	h := newLiveHarness(t)
	createLive(t, h)
	h.r.Config.Live = nil
	h.sp.detErr = errors.New("boom")
	h.now = time.Date(2026, 11, 16, 0, 5, 0, 0, time.UTC)
	h.r.RunOnce(context.Background()) // fails and backs off
	h.sp.detErr = nil
	got, err := h.r.Regenerate(context.Background(), Week, liveStart) // forced: ignores the backoff
	if err != nil || got.Live || h.sp.details["pl1"] == "" || got.Attempts != 0 {
		t.Fatalf("err = %v record = %+v", err, got)
	}
}
