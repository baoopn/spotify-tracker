package topplaylists

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/settings"
	"github.com/baoopn/spotify-tracker/internal/spotify"
)

var (
	liveWeek = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) // current week at the harness clock
	oct26    = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	oct19    = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	oct12    = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
)

// pruneHarness has a live week, last week and three older weeks published.
func pruneHarness(t *testing.T, keep int) *harness {
	t.Helper()
	h := newHarness(t, Week)
	h.r.Config.Live, h.r.Config.Keep = []Period{Week}, keep
	live := h.addCreated(Week, liveWeek, "live", 2)
	refreshed := h.now
	live.Live, live.RefreshedAt = true, &refreshed
	h.st.rows[key{"week", liveWeek.Unix()}] = live
	h.addCreated(Week, weekStart, "w1102", 3)
	hidden := h.addCreated(Week, oct26, "w1026", 3)
	hidden.Hidden = true
	h.st.rows[key{"week", oct26.Unix()}] = hidden
	h.addCreated(Week, oct19, "w1019", 3)
	h.addCreated(Week, oct12, "w1012", 3)
	return h
}

func TestPruneKeepsNewest(t *testing.T) {
	h := pruneHarness(t, 3)
	h.sp.ext.onUnfollow = func(id string) {
		for _, r := range h.st.rows {
			if r.SpotifyPlaylistID == id && r.Status != domain.TopPlaylistPruned {
				t.Errorf("%s unfollowed before it was marked pruned", id)
			}
		}
	}
	h.r.RunOnce(context.Background())

	if got := strings.Join(h.sp.ext.unfollows, ","); got != "w1012,w1019" {
		t.Fatalf("unfollows = %s, want oldest first w1012,w1019", got)
	}
	for _, s := range []time.Time{liveWeek, weekStart, oct26} { // the live and hidden ones count
		if r := h.row(Week, s); r.Status != domain.TopPlaylistCreated {
			t.Errorf("%s: status %s, want created", s.Format(time.DateOnly), r.Status)
		}
	}
	for _, s := range []time.Time{oct19, oct12} {
		r := h.row(Week, s)
		if r.Status != domain.TopPlaylistPruned || r.PrunedAt == nil || !r.SpotifyRemoved || r.Live || r.LastError != "" {
			t.Errorf("%s: row = %+v", s.Format(time.DateOnly), r)
		}
		if _, ok := h.st.snapshots[key{"week", s.Unix()}]; ok {
			t.Errorf("%s: snapshot not deleted", s.Format(time.DateOnly))
		}
	}
	if h.inval < 2 {
		t.Errorf("public cache invalidated %d times, want >= 2", h.inval)
	}

	// Nothing left to do on the next tick.
	h.r.RunOnce(context.Background())
	if len(h.sp.ext.unfollows) != 2 {
		t.Errorf("unfollows = %v", h.sp.ext.unfollows)
	}
}

func TestPruneUnfollowFailureRetried(t *testing.T) {
	h := pruneHarness(t, 4)
	h.sp.ext.unfollowErr = errors.New("spotify: remove playlist from library failed (500)")
	h.r.RunOnce(context.Background())
	r := h.row(Week, oct12)
	if r.Status != domain.TopPlaylistPruned || r.SpotifyRemoved || !strings.Contains(r.LastError, "remove from spotify") {
		t.Fatalf("row = %+v", r)
	}

	h.sp.ext.unfollowErr = nil
	h.r.RunOnce(context.Background())
	if r := h.row(Week, oct12); !r.SpotifyRemoved || r.LastError != "" || len(h.sp.ext.unfollows) != 1 {
		t.Fatalf("retry: row = %+v unfollows = %v", r, h.sp.ext.unfollows)
	}
}

func TestPruneAuthProblem(t *testing.T) {
	h := pruneHarness(t, 4)
	h.auth.s = settings.Spotify{RefreshToken: "rt", Scopes: []string{spotify.RequiredScope}}
	h.r.RunOnce(context.Background())
	r := h.row(Week, oct12)
	if r.Status != domain.TopPlaylistPruned || r.SpotifyRemoved || r.LastError != msgMissingScope {
		t.Fatalf("row = %+v", r)
	}
	if len(h.sp.ext.unfollows) != 0 {
		t.Error("no Spotify call expected without authorization")
	}

	h.auth.s.Scopes = nil
	h.sp.ext.unfollowErr = spotify.ErrInvalidGrant
	h.r.RunOnce(context.Background())
	if r := h.row(Week, oct12); r.LastError != msgRevoked {
		t.Fatalf("last_error = %q", r.LastError)
	}
}

func TestPruneRateLimitStopsTick(t *testing.T) {
	h := pruneHarness(t, 3)
	h.sp.ext.unfollowErr = &spotify.RateLimitError{RetryAfter: time.Minute}
	h.r.Config.Verify = time.Hour
	h.sp.ext.owned = map[string]bool{"x": true}
	h.r.RunOnce(context.Background())
	if h.sp.ext.ownedCalls != 0 {
		t.Error("verify ran after a rate limit")
	}
	if r := h.row(Week, oct19); r.Status != domain.TopPlaylistPruned {
		t.Errorf("status %s", r.Status)
	}
}

func TestPruneKeepZeroNeverPrunes(t *testing.T) {
	h := pruneHarness(t, 0)
	h.r.RunOnce(context.Background())
	if len(h.sp.ext.unfollows) != 0 || h.row(Week, oct12).Status != domain.TopPlaylistCreated {
		t.Fatal("Keep=0 pruned a playlist")
	}
}

func TestPruneOtherKindsUntouched(t *testing.T) {
	h := pruneHarness(t, 1)
	h.r.Config.Periods = []Period{Week, Month}
	h.addCreated(Month, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), "m10", 3)
	h.r.RunOnce(context.Background())
	if r := h.row(Month, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)); r.Status != domain.TopPlaylistCreated {
		t.Errorf("month status %s", r.Status)
	}
	if r := h.row(Week, liveWeek); r.Status != domain.TopPlaylistCreated {
		t.Errorf("live week status %s", r.Status)
	}
	if len(h.sp.ext.unfollows) != 4 {
		t.Errorf("unfollows = %v", h.sp.ext.unfollows)
	}
}

func TestGenerateSkipsPrunedPeriod(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Keep = 3
	h.st.ranked = ranked(3)
	h.st.rows[key{"week", weekStart.Unix()}] = domain.TopPlaylistRecord{Period: "week", PeriodStart: weekStart,
		Status: domain.TopPlaylistPruned, SpotifyPlaylistID: "old", SpotifyRemoved: true}
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 0 || h.week().Status != domain.TopPlaylistPruned {
		t.Fatalf("pruned period regenerated: creates=%v row=%+v", h.sp.creates, h.week())
	}
}

func TestGenerateTooOldAndPruned(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Keep = 2
	h.st.ranked = ranked(3)
	h.addCreated(Week, weekStart, "w1102", 3)
	h.addCreated(Week, oct26, "w1026", 3)
	ctx := context.Background()

	if _, err := h.r.Generate(ctx, Week, oct19); !errors.Is(err, ErrTooOld) {
		t.Fatalf("want ErrTooOld, got %v", err)
	}
	if len(h.sp.creates) != 0 {
		t.Fatal("too old period created a playlist")
	}
	if _, err := h.r.Generate(ctx, Week, oct26); err != nil { // a kept one regenerates
		t.Fatalf("regenerate kept: %v", err)
	}

	h.st.rows[key{"week", oct19.Unix()}] = domain.TopPlaylistRecord{Period: "week", PeriodStart: oct19,
		Status: domain.TopPlaylistPruned, SpotifyPlaylistID: "w1019"}
	if _, err := h.r.Regenerate(ctx, Week, oct19); !errors.Is(err, ErrPruned) {
		t.Fatalf("want ErrPruned, got %v", err)
	}

	h.r.Config.Keep = 0
	if _, err := h.r.Generate(ctx, Week, oct12); err != nil {
		t.Fatalf("Keep=0 backfill: %v", err)
	}
}
