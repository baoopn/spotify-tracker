package store_test

// Integration test against a real PostgreSQL for the admin tracks lyrics
// filter (see translations_pg_test.go for STORE_TEST_DATABASE_URL).

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/store"
)

func TestPGAdminTracksLyricsFilter(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "none", 0) // no lyrics row: never looked up
	bare(t, pool, "pend", 0)
	exec(t, pool, `INSERT INTO lyrics (track_id, status) VALUES ('pend', 'pending')`)
	bare(t, pool, "nf", 0)
	exec(t, pool, `INSERT INTO lyrics (track_id, status) VALUES ('nf', 'not_found')`)
	bare(t, pool, "err", 0)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, last_error) VALUES ('err', 'error', 'boom')`)
	seed(t, pool, "syn", time.Hour) // found with synced lyrics
	bare(t, pool, "plain", 0)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, plain_lyrics, synced_lyrics) VALUES ('plain', 'found', 'một', '')`)
	bare(t, pool, "ins", 0)
	exec(t, pool, `INSERT INTO lyrics (track_id, status) VALUES ('ins', 'instrumental')`)

	cases := []struct {
		filter string
		want   []string
	}{
		{"", []string{"err", "ins", "nf", "none", "pend", "plain", "syn"}},
		{store.LyricsFilterMissing, []string{"err", "nf", "none", "pend"}},
		{store.LyricsFilterUnsynced, []string{"plain"}},
		{store.LyricsFilterFound, []string{"plain", "syn"}},
		{store.LyricsFilterInstrumental, []string{"ins"}},
	}
	for _, tc := range cases {
		rows, total, err := st.AdminTracks(ctx, "", "", tc.filter, "name", "asc", 50, 0)
		if err != nil {
			t.Fatalf("%q: %v", tc.filter, err)
		}
		got := make([]string, len(rows))
		for i, r := range rows {
			got[i] = r.Track.ID
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) || total != int64(len(tc.want)) {
			t.Errorf("%q = %v (total %d), want %v", tc.filter, got, total, tc.want)
		}
	}

	if n, err := st.LyricsMissingCount(ctx); err != nil || n != 4 {
		t.Errorf("LyricsMissingCount = %d, %v; want 4", n, err)
	}

	rows, _, err := st.AdminTracks(ctx, "", "", "", "name", "asc", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	type state struct {
		status string
		synced bool
	}
	want := map[string]state{
		"none": {"pending", false}, "pend": {"pending", false}, "nf": {"not_found", false},
		"err": {"error", false}, "syn": {"found", true}, "plain": {"found", false}, "ins": {"instrumental", false},
	}
	for _, r := range rows {
		if got := (state{r.LyricsStatus, r.LyricsSynced}); got != want[r.Track.ID] {
			t.Errorf("%s = %+v, want %+v", r.Track.ID, got, want[r.Track.ID])
		}
	}
}
