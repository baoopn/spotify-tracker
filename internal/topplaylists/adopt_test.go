package topplaylists

import (
	"context"
	"errors"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// A create that reached Spotify but whose response was lost is adopted on
// the retry instead of creating a second public playlist.
func TestRunnerAdoptsPlaylistAfterLostCreate(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(3)
	h.sp.ext.loseCreates = 1
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistFailed || w.Attempts != 1 || w.SpotifyPlaylistID != "" {
		t.Fatalf("after lost create: row = %+v", w)
	}
	if h.sp.ext.findCalls != 0 {
		t.Errorf("first attempt looked up the library %d times", h.sp.ext.findCalls)
	}

	h.now = *h.week().NextAttemptAt
	h.r.RunOnce(context.Background())
	w := h.week()
	if w.Status != domain.TopPlaylistCreated || w.SpotifyPlaylistID != "pl1" {
		t.Fatalf("after retry: row = %+v", w)
	}
	if len(h.sp.creates) != 1 || h.sp.ext.findCalls != 1 {
		t.Fatalf("creates = %v, finds = %d; want one create and one lookup", h.sp.creates, h.sp.ext.findCalls)
	}
	if got := h.sp.replaces["pl1"]; len(got) != 3 {
		t.Errorf("adopted playlist items = %v", got)
	}
}

// With nothing to adopt the retry creates the playlist; a failed lookup is
// a failed attempt, never a blind create.
func TestRunnerRetryCreatesWhenNothingToAdopt(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	h.sp.createErr = errors.New("spotify: create playlist failed (500)")
	h.r.RunOnce(context.Background())
	h.sp.createErr = nil

	h.sp.ext.findErr = errors.New("spotify: my playlists failed (500)")
	h.now = *h.week().NextAttemptAt
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistFailed || w.Attempts != 2 || len(h.sp.creates) != 0 {
		t.Fatalf("lookup failure: row = %+v, creates = %v", w, h.sp.creates)
	}

	h.sp.ext.findErr = nil
	h.now = *h.week().NextAttemptAt
	h.r.RunOnce(context.Background())
	if w := h.week(); w.Status != domain.TopPlaylistCreated || w.SpotifyPlaylistID != "pl1" || len(h.sp.creates) != 1 {
		t.Fatalf("retry: row = %+v, creates = %v", w, h.sp.creates)
	}
}

// A pending row without an ID (the process died mid-create) adopts the
// playlist even with no failed attempt, and the final attempt replaces the
// live description it may carry.
func TestRunnerAdoptsAfterCrashAndUpdatesDetails(t *testing.T) {
	h := newHarness(t, Week)
	h.st.ranked = ranked(2)
	name := Name(Week, weekStart)
	h.st.rows[key{"week", weekStart.Unix()}] = domain.TopPlaylistRecord{Period: "week", PeriodStart: weekStart,
		PeriodEnd: weekStart.AddDate(0, 0, 7), TimeZone: "UTC", Name: name, Status: domain.TopPlaylistPending}
	h.sp.ext.named = map[string]string{name: "orphan"}
	h.r.RunOnce(context.Background())

	w := h.week()
	if w.Status != domain.TopPlaylistCreated || w.SpotifyPlaylistID != "orphan" || len(h.sp.creates) != 0 {
		t.Fatalf("row = %+v, creates = %v", w, h.sp.creates)
	}
	if got := h.sp.details["orphan"]; got != name+"|"+Description(Week, weekStart) {
		t.Errorf("details of the adopted playlist = %q", got)
	}
}
