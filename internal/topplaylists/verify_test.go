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

// verifyHarness has last week (w1102) and the week before (w1026) published
// and checks enabled every hour.
func verifyHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, Week)
	h.r.Config.Verify = time.Hour
	h.addCreated(Week, weekStart, "w1102", 3)
	h.addCreated(Week, oct26, "w1026", 2)
	return h
}

func TestVerifyAllPresent(t *testing.T) {
	h := verifyHarness(t)
	h.sp.ext.owned = map[string]bool{"w1102": true, "w1026": true, "other": true}
	h.r.RunOnce(context.Background())
	if h.sp.ext.ownedCalls != 1 || len(h.sp.ext.follows) != 0 || len(h.sp.creates) != 0 {
		t.Fatalf("owned=%d follows=%v creates=%v", h.sp.ext.ownedCalls, h.sp.ext.follows, h.sp.creates)
	}
	if r := h.week(); r.VerifiedAt == nil || !r.VerifiedAt.Equal(h.now) || r.RestoredAt != nil {
		t.Fatalf("row = %+v", r)
	}
}

func TestVerifyRefollowsMissing(t *testing.T) {
	h := verifyHarness(t)
	h.sp.ext.owned = map[string]bool{"w1026": true}
	rec := h.week()
	rec.LastError = "old error"
	h.st.rows[key{"week", weekStart.Unix()}] = rec
	h.r.RunOnce(context.Background())

	if strings.Join(h.sp.ext.follows, ",") != "w1102" || len(h.sp.creates) != 0 {
		t.Fatalf("follows=%v creates=%v", h.sp.ext.follows, h.sp.creates)
	}
	if got := h.sp.replaces["w1102"]; len(got) != 3 || got[0] != "spotify:track:t1" {
		t.Errorf("items not restored: %v", got)
	}
	r := h.week()
	if r.RestoredAt == nil || r.LastError != "" || r.Status != domain.TopPlaylistCreated || r.SpotifyPlaylistID != "w1102" {
		t.Fatalf("row = %+v", r)
	}
	if !strings.Contains(h.logs.String(), "top playlist restored (re-followed)") || h.inval == 0 {
		t.Errorf("missing log or invalidate: %s", h.logs)
	}
}

func TestVerifyRecreatesGonePlaylist(t *testing.T) {
	h := verifyHarness(t)
	h.sp.ext.owned = map[string]bool{"w1026": true}
	h.sp.ext.followErr = fmt.Errorf("%w: follow playlist", spotify.ErrNotFound)
	failSave := true
	h.st.upsertErr = func(p domain.TopPlaylistRecord) error {
		if failSave && p.SpotifyPlaylistID == "pl1" {
			return errors.New("db down")
		}
		return nil
	}
	ctx := context.Background()
	h.r.RunOnce(ctx)
	if len(h.sp.creates) != 1 || h.week().SpotifyPlaylistID != "w1102" ||
		!strings.Contains(h.week().LastError, "restore failed") {
		t.Fatalf("creates=%v row=%+v", h.sp.creates, h.week())
	}

	failSave = false
	if err := h.r.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	r := h.week()
	if len(h.sp.creates) != 1 {
		t.Fatalf("duplicate playlist created: %v", h.sp.creates)
	}
	if r.SpotifyPlaylistID != "pl1" || r.SpotifyURL != "https://open.spotify.com/playlist/pl1" ||
		r.RestoredAt == nil || r.LastError != "" || r.Name != Name(Week, weekStart) {
		t.Fatalf("row = %+v", r)
	}
	if got := h.sp.replaces["pl1"]; len(got) != 3 {
		t.Errorf("items = %v", got)
	}
	if h.sp.descrs[0] != Description(Week, weekStart) {
		t.Errorf("description = %q", h.sp.descrs[0])
	}
	if h.inval == 0 || !strings.Contains(h.logs.String(), "top playlist restored (recreated)") {
		t.Error("cache not invalidated or restore not logged")
	}
}

func TestVerifyListFailures(t *testing.T) {
	for name, set := range map[string]func(h *harness){
		"list error": func(h *harness) { h.sp.ext.ownedErr = errors.New("spotify: get my playlists failed (500)") },
		"empty list": func(h *harness) { h.sp.ext.owned = map[string]bool{} },
	} {
		t.Run(name, func(t *testing.T) {
			h := verifyHarness(t)
			set(h)
			h.r.RunOnce(context.Background())
			if len(h.sp.ext.follows) != 0 || len(h.sp.creates) != 0 || h.st.ext.verified != 0 {
				t.Fatalf("follows=%v creates=%v verified=%d", h.sp.ext.follows, h.sp.creates, h.st.ext.verified)
			}
			if !strings.Contains(h.logs.String(), "level=WARN") {
				t.Errorf("no warning logged: %s", h.logs)
			}
		})
	}
}

func TestVerifyInterval(t *testing.T) {
	h := verifyHarness(t)
	h.sp.ext.owned = map[string]bool{"w1102": true, "w1026": true}
	ctx := context.Background()
	h.r.RunOnce(ctx)
	h.now = h.now.Add(30 * time.Minute)
	h.r.RunOnce(ctx)
	if h.sp.ext.ownedCalls != 1 {
		t.Fatalf("checked %d times within the interval", h.sp.ext.ownedCalls)
	}
	h.now = h.now.Add(31 * time.Minute)
	h.r.RunOnce(ctx)
	if h.sp.ext.ownedCalls != 2 {
		t.Fatalf("checked %d times, want 2 after the interval", h.sp.ext.ownedCalls)
	}
	if err := h.r.Verify(ctx); err != nil || h.sp.ext.ownedCalls != 3 { // forced ignores it
		t.Fatalf("forced verify: %v calls=%d", err, h.sp.ext.ownedCalls)
	}

	h.r.Config.Verify = 0 // off
	h.now = h.now.Add(48 * time.Hour)
	h.r.RunOnce(ctx)
	if h.sp.ext.ownedCalls != 3 {
		t.Error("verify ran while disabled")
	}
	h.st.locked = true
	if err := h.r.Verify(ctx); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
}

func TestVerifyMaxRestoresPerCheck(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Verify = time.Hour
	for i := range 5 {
		h.addCreated(Week, oct12.AddDate(0, 0, -7*i), fmt.Sprintf("old%d", i), 1)
	}
	h.addCreated(Week, weekStart, "w1102", 1)
	h.sp.ext.owned = map[string]bool{"w1102": true}
	h.r.RunOnce(context.Background())
	if len(h.sp.ext.follows) != restoreBatch {
		t.Fatalf("follows = %v, want %d", h.sp.ext.follows, restoreBatch)
	}
}

func TestVerifyOtherFailureKeepsCreated(t *testing.T) {
	h := verifyHarness(t)
	h.sp.ext.owned = map[string]bool{"w1026": true}
	h.sp.ext.followErr = errors.New("spotify: save playlist to library failed (500)")
	h.r.RunOnce(context.Background())
	r := h.week()
	if r.Status != domain.TopPlaylistCreated || r.SpotifyPlaylistID != "w1102" ||
		!strings.HasPrefix(r.LastError, "restore failed: ") || r.RestoredAt != nil {
		t.Fatalf("row = %+v", r)
	}
}

func TestLiveRefreshRecreatesDeletedPlaylist(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Live = []Period{Week}
	live := h.addCreated(Week, liveWeek, "live", 2)
	old := h.now.Add(-2 * time.Hour)
	live.Live, live.RefreshedAt = true, &old
	h.st.rows[key{"week", liveWeek.Unix()}] = live
	h.addCreated(Week, weekStart, "w1102", 1) // last week, done
	h.st.ranked = []domain.RankedTrack{{TrackID: "t9", Plays: 9}}
	notFound := fmt.Errorf("%w: replace playlist items", spotify.ErrNotFound)
	h.sp.ext.replErrFor = func(id string) error {
		if id == "live" {
			return notFound
		}
		return nil
	}
	h.sp.ext.followErr = notFound
	h.r.RunOnce(context.Background())

	r := h.row(Week, liveWeek)
	if len(h.sp.creates) != 1 || r.SpotifyPlaylistID != "pl1" || r.Status != domain.TopPlaylistCreated ||
		!r.Live || r.RestoredAt == nil {
		t.Fatalf("creates=%v row=%+v", h.sp.creates, r)
	}
	if !strings.HasPrefix(h.sp.descrs[0], "The 50 most-played tracks of the week of Nov 9, 2026 so far") {
		t.Errorf("description = %q", h.sp.descrs[0])
	}
	if got := h.sp.replaces["pl1"]; len(got) != 2 { // the stored snapshot
		t.Errorf("items = %v", got)
	}

	// The next tick refreshes the recreated playlist.
	h.r.RunOnce(context.Background())
	if got := h.sp.replaces["pl1"]; len(got) != 1 || got[0] != "spotify:track:t9" || len(h.sp.creates) != 1 {
		t.Fatalf("items = %v creates = %v", got, h.sp.creates)
	}
}
