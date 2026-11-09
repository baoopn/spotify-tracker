package topplaylists

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/spotify"
)

func TestOwnerName(t *testing.T) {
	if got := OwnerName("alex", Week, weekStart); got != "alex's Top 50 · Week of Nov 2, 2026" {
		t.Errorf("got %q", got)
	}
	if got := OwnerName("", Month, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); got != "Top 50 · November 2026" {
		t.Errorf("got %q", got)
	}
}

func TestOwnerConfigWins(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Owner = "alex"
	h.sp.ext.userName = "someone else"
	h.st.ranked = ranked(2)
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 1 || h.sp.creates[0] != "alex's Top 50 · Week of Nov 2, 2026" || h.week().Name != h.sp.creates[0] {
		t.Fatalf("creates = %v", h.sp.creates)
	}
	if h.sp.ext.userCalls != 0 {
		t.Error("Spotify profile read although the owner is configured")
	}
	if name, src := h.r.Owner(); name != "alex" || src != OwnerSourceConfig {
		t.Errorf("Owner() = %q %q", name, src)
	}
}

func TestOwnerFromSpotifyCached(t *testing.T) {
	h := newHarness(t, Week)
	h.sp.ext.userName = "  alex  "
	h.st.ranked = ranked(2)
	ctx := context.Background()
	if name, src := h.r.Owner(); name != "" || src != OwnerSourceNone {
		t.Errorf("before a tick Owner() = %q %q", name, src)
	}
	h.r.RunOnce(ctx)
	if len(h.sp.creates) != 1 || h.sp.creates[0] != "alex's Top 50 · Week of Nov 2, 2026" {
		t.Fatalf("creates = %v", h.sp.creates)
	}
	if name, src := h.r.Owner(); name != "alex" || src != OwnerSourceSpotify {
		t.Errorf("Owner() = %q %q", name, src)
	}

	h.now = h.now.Add(23 * time.Hour)
	h.r.RunOnce(ctx)
	if h.sp.ext.userCalls != 1 {
		t.Fatalf("profile read %d times within 24h", h.sp.ext.userCalls)
	}
	// Expired and the read fails: the cached name keeps being used.
	h.now = h.now.Add(2 * time.Hour)
	h.sp.ext.userErr = errors.New("spotify: get current user failed (500)")
	h.r.RunOnce(ctx)
	if name, src := h.r.Owner(); h.sp.ext.userCalls != 2 || name != "alex" || src != OwnerSourceSpotify {
		t.Fatalf("calls=%d Owner() = %q %q", h.sp.ext.userCalls, name, src)
	}
	h.sp.ext.userErr, h.sp.ext.userName = nil, "sam"
	h.r.RunOnce(ctx)
	if name, _ := h.r.Owner(); name != "sam" {
		t.Errorf("Owner() = %q after a successful read", name)
	}
}

func TestOwnerUnresolvedPlainNamesNoRenames(t *testing.T) {
	h := newHarness(t, Week)
	h.addCreated(Week, oct26, "w1026", 2)
	rec := h.row(Week, oct26)
	rec.Name = "alex's Top 50 · Week of Oct 26, 2026"
	h.st.rows[key{"week", oct26.Unix()}] = rec
	h.sp.ext.userErr = errors.New("spotify: not connected")
	h.st.ranked = ranked(2)
	h.r.RunOnce(context.Background())

	if len(h.sp.creates) != 1 || h.sp.creates[0] != "Top 50 · Week of Nov 2, 2026" {
		t.Fatalf("creates = %v", h.sp.creates)
	}
	if h.sp.ext.detailCalls != 0 || h.row(Week, oct26).Name != rec.Name {
		t.Fatalf("renamed while the owner is unresolved: details=%v", h.sp.details)
	}
	if name, src := h.r.Owner(); name != "" || src != OwnerSourceNone {
		t.Errorf("Owner() = %q %q", name, src)
	}
}

func TestRenameOnOwnerChange(t *testing.T) {
	h := newHarness(t, Week)
	h.r.Config.Live = []Period{Week}
	h.addCreated(Week, weekStart, "w1102", 2)
	live := h.addCreated(Week, liveWeek, "live", 2)
	refreshed := h.now
	live.Live, live.RefreshedAt = true, &refreshed
	h.st.rows[key{"week", liveWeek.Unix()}] = live
	h.r.Config.Owner = "alex"
	ctx := context.Background()
	h.r.RunOnce(ctx)

	if got := h.sp.details["w1102"]; got != "alex's Top 50 · Week of Nov 2, 2026|"+Description(Week, weekStart) {
		t.Errorf("finished details = %q", got)
	}
	if got := h.sp.details["live"]; got != "alex's Top 50 · Week of Nov 9, 2026|"+LiveDescription(Week, liveWeek, time.Hour) {
		t.Errorf("live details = %q", got)
	}
	if h.week().Name != "alex's Top 50 · Week of Nov 2, 2026" || h.inval < 2 {
		t.Fatalf("stored name %q inval=%d", h.week().Name, h.inval)
	}

	calls := h.sp.ext.detailCalls
	h.r.RunOnce(ctx) // names match: no Spotify call
	if h.sp.ext.detailCalls != calls {
		t.Errorf("details updated again (%d calls)", h.sp.ext.detailCalls-calls)
	}
}

func TestRenameFailureAndBatch(t *testing.T) {
	h := newHarness(t, Week)
	for i := range 12 {
		h.addCreated(Week, weekStart.AddDate(0, 0, -7*i), fmt.Sprintf("w%d", i), 1)
	}
	h.r.Config.Owner = "alex"
	h.sp.detErr = errors.New("spotify: update playlist details failed (500)")
	ctx := context.Background()
	h.r.RunOnce(ctx)
	if h.sp.ext.detailCalls != renameMaxCalls {
		t.Fatalf("%d rename calls, want %d", h.sp.ext.detailCalls, renameMaxCalls)
	}
	for _, r := range h.st.rows {
		if strings.HasPrefix(r.Name, "alex") {
			t.Fatalf("failed rename stored: %+v", r)
		}
	}
	oldest := weekStart.AddDate(0, 0, -77)
	if !strings.HasPrefix(h.row(Week, oldest).LastError, "rename: ") {
		t.Errorf("last_error = %q", h.row(Week, oldest).LastError)
	}

	// The five oldest keep failing; they must not starve the others.
	h.sp.detErr = nil
	h.sp.ext.detErrFor = func(id string) error {
		if failing := map[string]bool{"w7": true, "w8": true, "w9": true, "w10": true, "w11": true}; failing[id] {
			return errors.New("spotify: update playlist details failed (500)")
		}
		return nil
	}
	h.sp.ext.detailCalls = 0
	h.r.RunOnce(ctx)
	renamed := 0
	for _, r := range h.st.rows {
		if strings.HasPrefix(r.Name, "alex's Top 50 · ") {
			renamed++
		}
	}
	if renamed != renameBatch || h.sp.ext.detailCalls != renameMaxCalls {
		t.Fatalf("renamed %d with %d calls, want %d with %d", renamed, h.sp.ext.detailCalls, renameBatch, renameMaxCalls)
	}

	h.sp.ext.detErrFor = nil
	h.r.RunOnce(ctx)
	h.r.RunOnce(ctx)
	for _, r := range h.st.rows {
		if !strings.HasPrefix(r.Name, "alex's Top 50 · ") || r.LastError != "" {
			t.Fatalf("not renamed: %+v", r)
		}
	}
}

func TestRenameRefollowedPlaylistRenamedOnSpotify(t *testing.T) {
	h := newHarness(t, Week)
	h.addCreated(Week, weekStart, "w1102", 2)
	h.r.Config.Owner = "alex"
	first := true
	h.sp.ext.detErrFor = func(string) error {
		if first {
			first = false
			return fmt.Errorf("%w: update playlist details", spotify.ErrNotFound)
		}
		return nil
	}
	h.r.RunOnce(context.Background())
	want := "alex's Top 50 · Week of Nov 2, 2026"
	if strings.Join(h.sp.ext.follows, ",") != "w1102" || len(h.sp.creates) != 0 {
		t.Fatalf("follows=%v creates=%v", h.sp.ext.follows, h.sp.creates)
	}
	if got := h.sp.details["w1102"]; !strings.HasPrefix(got, want+"|") {
		t.Fatalf("Spotify name = %q", got)
	}
	if r := h.week(); r.Name != want || r.RestoredAt == nil || r.LastError != "" {
		t.Fatalf("row = %+v", r)
	}
}

func TestOwnerNotConnectedSkipsProfile(t *testing.T) {
	h := newHarness(t, Week)
	h.auth.s.RefreshToken = ""
	h.sp.ext.userName = "alex"
	h.r.RunOnce(context.Background())
	if h.sp.ext.userCalls != 0 || strings.Contains(h.logs.String(), "display name") {
		t.Fatalf("profile read while not connected (calls=%d): %s", h.sp.ext.userCalls, h.logs)
	}
	if name, src := h.r.Owner(); name != "" || src != OwnerSourceNone {
		t.Errorf("Owner() = %q %q", name, src)
	}
}

func TestRenameGonePlaylistRestores(t *testing.T) {
	h := newHarness(t, Week)
	h.addCreated(Week, weekStart, "w1102", 2)
	h.r.Config.Owner = "alex"
	h.sp.detErr = fmt.Errorf("%w: update playlist details", spotify.ErrNotFound)
	h.sp.ext.followErr = h.sp.detErr
	h.r.RunOnce(context.Background())
	if len(h.sp.creates) != 1 || h.sp.creates[0] != "alex's Top 50 · Week of Nov 2, 2026" ||
		h.week().SpotifyPlaylistID != "pl1" || h.week().Name != h.sp.creates[0] {
		t.Fatalf("creates=%v row=%+v", h.sp.creates, h.week())
	}
}

func TestParseOptions(t *testing.T) {
	for in, want := range map[string]int{"": 3, "3": 3, "\t5\n": 5, "1": 1, "0": 0} {
		if got, err := ParseKeep(in); err != nil || got != want {
			t.Errorf("ParseKeep(%q) = %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"-1", "abc", "2.5"} {
		if got, err := ParseKeep(in); err == nil || got != DefaultKeep {
			t.Errorf("ParseKeep(%q) = %d %v, want error and %d", in, got, err, DefaultKeep)
		}
	}
	for in, want := range map[string]time.Duration{"": time.Hour, "1h": time.Hour, "10m": 10 * time.Minute,
		"0": 0, "off": 0, "OFF": 0, "0s": 0, "6h": 6 * time.Hour} {
		if got, err := ParseVerify(in); err != nil || got != want {
			t.Errorf("ParseVerify(%q) = %s %v, want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"5m", "soon", "-1h"} {
		if got, err := ParseVerify(in); err == nil || got != DefaultVerify {
			t.Errorf("ParseVerify(%q) = %s %v, want error and %s", in, got, err, DefaultVerify)
		}
	}
	if got, err := ParseOwner("  alex "); err != nil || got != "alex" {
		t.Errorf("ParseOwner = %q %v", got, err)
	}
	long := strings.Repeat("é", 45)
	if got, err := ParseOwner(long); err == nil || got != strings.Repeat("é", 40) {
		t.Errorf("ParseOwner(long) = %q %v", got, err)
	}
}
