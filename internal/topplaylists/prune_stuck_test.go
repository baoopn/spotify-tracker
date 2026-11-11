package topplaylists

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// A failed or waiting_auth period with a Spotify playlist is no longer
// generated once newer periods exist; prune removes its playlist when it
// has Keep newer created playlists, and leaves newer stuck rows alone.
func TestPruneStuckRows(t *testing.T) {
	h := pruneHarness(t, 3)
	oct5 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	sep28 := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	for _, s := range []struct {
		start  time.Time
		id     string
		status string
	}{
		{oct5, "failed-old", domain.TopPlaylistFailed},
		{sep28, "waiting-old", domain.TopPlaylistWaitingAuth},
	} {
		rec := h.addCreated(Week, s.start, s.id, 2)
		rec.Status, rec.Attempts = s.status, 3
		h.st.rows[key{"week", s.start.Unix()}] = rec
	}
	// A stuck row with no Spotify playlist has nothing to remove.
	noID := domain.TopPlaylistRecord{Period: "week", PeriodStart: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
		Status: domain.TopPlaylistFailed}
	h.st.rows[key{"week", noID.PeriodStart.Unix()}] = noID
	// The current failing period stays: it has fewer than Keep newer ones.
	failing := h.row(Week, weekStart)
	later := h.now.Add(time.Hour)
	failing.Status, failing.Attempts, failing.NextAttemptAt = domain.TopPlaylistFailed, 1, &later
	h.st.rows[key{"week", weekStart.Unix()}] = failing
	h.r.Config.Keep = 2

	h.r.RunOnce(context.Background())
	for _, s := range []time.Time{oct5, sep28} {
		if r := h.row(Week, s); r.Status != domain.TopPlaylistPruned || !r.SpotifyRemoved {
			t.Errorf("%s: row = %+v", s.Format(time.DateOnly), r)
		}
	}
	got := h.sp.ext.unfollows
	for _, id := range []string{"failed-old", "waiting-old"} {
		if !strings.Contains(strings.Join(got, ","), id) {
			t.Errorf("%s not removed from spotify: %v", id, got)
		}
	}
	if r := h.row(Week, weekStart); r.Status != domain.TopPlaylistFailed {
		t.Errorf("current failing period pruned: %+v", r)
	}
	if r := h.row(Week, noID.PeriodStart); r.Status != domain.TopPlaylistFailed {
		t.Errorf("row without a playlist pruned: %+v", r)
	}
}
