package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/spotify-tracker/internal/store"
)

type playRow struct {
	heard   int
	counted bool
}

func plays(t *testing.T, pool *pgxpool.Pool, listenerID int64) []playRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT heard_ms, counted FROM listener_plays WHERE listener_id = $1 ORDER BY id`, listenerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []playRow
	for rows.Next() {
		var r playRow
		if err := rows.Scan(&r.heard, &r.counted); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestPGListenerPlays(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `TRUNCATE listeners CASCADE`)
	exec(t, pool, `INSERT INTO artists (id, name) VALUES ('ar', 'Artist') ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`)
	exec(t, pool, `INSERT INTO albums (id, name, cover_image_url) VALUES ('lp_al', 'Album', 'https://img.example.com/c.jpg')
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, cover_image_url = EXCLUDED.cover_image_url`)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms, album_id) VALUES ('long', 'Long', 200000, 'lp_al'), ('short', 'Short', 40000, NULL)`)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('long', 'ar', 0)`)
	newListener := func(email string) store.Listener {
		t.Helper()
		l, err := st.CreateListener(ctx, store.NewListener{Email: email, InvitedBy: "a"})
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	record := func(id int64, track string, delta, dur int64) {
		t.Helper()
		if err := st.RecordListenerProgress(ctx, id, track, delta, dur); err != nil {
			t.Fatal(err)
		}
	}
	extend := func(id int64, track string, delta int64) {
		t.Helper()
		if err := st.ExtendListenerProgress(ctx, id, track, delta); err != nil {
			t.Fatal(err)
		}
	}
	// ago moves a listener's plays into the past (both start and last report).
	ago := func(id int64, start, last string) {
		t.Helper()
		exec(t, pool, `UPDATE listener_plays SET started_at = now() - $2::interval, last_heard_at = now() - $3::interval
			WHERE listener_id = $1`, id, start, last)
	}
	l := newListener("ann@example.com")

	// New row: the delta as is, not counted yet.
	record(l.ID, "long", 15000, 200000)
	// Right away again: at most 5 s more (anti-inflation).
	record(l.ID, "long", 15000, 200000)
	if got := plays(t, pool, l.ID); len(got) != 1 || got[0].heard < 20000 || got[0].heard > 20100 || got[0].counted {
		t.Fatalf("after clamp = %+v", got)
	}
	// A minute later: the full delta, and it reaches 30 s.
	ago(l.ID, "60 seconds", "60 seconds")
	record(l.ID, "long", 15000, 200000)
	if got := plays(t, pool, l.ID); len(got) != 1 || got[0].heard < 35000 || got[0].heard > 35100 || !got[0].counted {
		t.Fatalf("counted = %+v", got)
	}
	// After the 2-minute window: a new row.
	ago(l.ID, "4 minutes", "3 minutes")
	record(l.ID, "long", 1000, 200000)
	if got := plays(t, pool, l.ID); len(got) != 2 || got[1].heard != 1000 || got[1].counted {
		t.Fatalf("new row = %+v", got)
	}
	// A short song counts at half its length (20 s of 40 s).
	record(l.ID, "short", 20000, 40000)
	if got := plays(t, pool, l.ID); len(got) != 3 || !got[2].counted {
		t.Fatalf("short = %+v", got)
	}
	// Unknown track or listener: ignored.
	record(l.ID, "missing", 20000, 0)
	record(999999, "long", 20000, 0)
	if got := plays(t, pool, l.ID); len(got) != 3 {
		t.Fatalf("ignored reports wrote rows: %+v", got)
	}

	// Rapid reports: the 5 s slack applies per report, but a play never holds
	// more than the time since it started plus 20 s, so it cannot reach 30 s
	// in a second.
	fast := newListener("dee@example.com")
	for range 10 {
		record(fast.ID, "long", 20000, 200000)
	}
	if got := plays(t, pool, fast.ID); len(got) != 1 || got[0].heard > 22000 || got[0].counted {
		t.Fatalf("rapid reports = %+v", got)
	}

	// A late flush for a song no longer playing: it extends a play heard in
	// the last 30 s (same clamp and cap, count rule from the catalog length)
	// and never creates one.
	late := newListener("cy@example.com")
	extend(late.ID, "short", 15000)
	if got := plays(t, pool, late.ID); len(got) != 0 {
		t.Fatalf("late flush created a play: %+v", got)
	}
	record(late.ID, "short", 15000, 40000)
	ago(late.ID, "10 seconds", "10 seconds")
	extend(late.ID, "short", 15000)
	// 15 s + min(15 s, 10 s + 5 s) = 30 s (the cap is 10 s + 20 s), past half
	// of the 40 s song.
	if got := plays(t, pool, late.ID); len(got) != 1 || got[0].heard < 29900 || got[0].heard > 30100 || !got[0].counted {
		t.Fatalf("late extend = %+v", got)
	}
	// The window does not slide: a late report keeps last_heard_at, so it
	// still counts from the last report of the current song.
	var kept bool
	if err := pool.QueryRow(ctx, `SELECT last_heard_at < now() - interval '9 seconds' FROM listener_plays WHERE listener_id = $1`,
		late.ID).Scan(&kept); err != nil || !kept {
		t.Errorf("late report moved last_heard_at: %v %v", kept, err)
	}
	// More late reports add nothing past the cap.
	extend(late.ID, "short", 15000)
	extend(late.ID, "short", 15000)
	if got := plays(t, pool, late.ID); len(got) != 1 || got[0].heard > 30100 {
		t.Fatalf("late reports past the cap = %+v", got)
	}
	ago(late.ID, "60 seconds", "40 seconds")
	extend(late.ID, "short", 15000)
	if got := plays(t, pool, late.ID); len(got) != 1 || got[0].heard > 30100 {
		t.Fatalf("stale late flush extended: %+v", got)
	}

	// Concurrent reports for a new play: one row, no double count.
	other := newListener("bob@example.com")
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := st.RecordListenerProgress(ctx, other.ID, "long", 10000, 200000); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := plays(t, pool, other.ID); len(got) != 1 || got[0].heard > 10000+4*5100 || got[0].counted {
		t.Fatalf("concurrent = %+v", got)
	}

	// A revoked listener records nothing.
	if _, err := st.RevokeListener(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	record(other.ID, "short", 20000, 40000)
	if got := plays(t, pool, other.ID); len(got) != 1 {
		t.Fatalf("revoked listener recorded: %+v", got)
	}

	// History: counted rows only, newest first, own rows only, paged. Ann has
	// two counted plays (long, then short) and one that did not count.
	h, next, err := st.ListenerHistory(ctx, l.ID, 1, "")
	if err != nil || len(h) != 1 || next == nil || h[0].Track.ID != "short" {
		t.Fatalf("page 1 = %+v %v %v", h, next, err)
	}
	h, next2, err := st.ListenerHistory(ctx, l.ID, 1, *next)
	if err != nil || len(h) != 1 || next2 != nil || h[0].Track.ID != "long" {
		t.Fatalf("page 2 = %+v %v %v", h, next2, err)
	}
	tr := h[0].Track
	if tr.Name != "Long" || tr.AlbumName != "Album" || tr.CoverImageURL != "https://img.example.com/c.jpg" ||
		len(tr.Artists) != 1 || tr.Artists[0].Name != "Artist" || h[0].HeardMS < 35000 {
		t.Errorf("play = %+v", h[0])
	}
	if h, next, err := st.ListenerHistory(ctx, l.ID, 2, ""); err != nil || len(h) != 2 || next != nil {
		t.Errorf("one page of 2 = %+v %v %v", h, next, err)
	}
	if h, _, _ := st.ListenerHistory(ctx, other.ID, 50, ""); len(h) != 0 {
		t.Errorf("other listener sees %d rows (none counted)", len(h))
	}
	if _, _, err := st.ListenerHistory(ctx, l.ID, 10, "%%%"); !errors.Is(err, store.ErrBadCursor) {
		t.Errorf("bad cursor err = %v", err)
	}

	// Deleting a listener deletes their plays.
	exec(t, pool, `DELETE FROM listeners WHERE id = $1`, l.ID)
	if got := plays(t, pool, l.ID); len(got) != 0 {
		t.Errorf("plays survived delete: %+v", got)
	}
}
