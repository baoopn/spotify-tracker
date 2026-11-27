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
	exec(t, pool, `INSERT INTO artists (id, name) VALUES ('ar', 'Artist')`)
	exec(t, pool, `INSERT INTO albums (id, name, cover_image_url) VALUES ('al', 'Album', 'https://img.example.com/c.jpg')`)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms, album_id) VALUES ('long', 'Long', 200000, 'al'), ('short', 'Short', 40000, NULL)`)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('long', 'ar', 0)`)
	l, err := st.CreateListener(ctx, store.NewListener{Email: "ann@example.com", InvitedBy: "a"})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := st.CreateListener(ctx, store.NewListener{Email: "bob@example.com", InvitedBy: "a"})

	// New row: the delta as is, not counted yet.
	if err := st.RecordListenerProgress(ctx, l.ID, "long", 15000, 200000); err != nil {
		t.Fatal(err)
	}
	// Right away again: at most 5 s more (anti-inflation).
	if err := st.RecordListenerProgress(ctx, l.ID, "long", 15000, 200000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, l.ID); len(got) != 1 || got[0].heard < 20000 || got[0].heard > 20100 || got[0].counted {
		t.Fatalf("after clamp = %+v", got)
	}
	// A minute later: the full delta, and it reaches 30 s.
	exec(t, pool, `UPDATE listener_plays SET last_heard_at = now() - interval '60 seconds'`)
	if err := st.RecordListenerProgress(ctx, l.ID, "long", 15000, 200000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, l.ID); len(got) != 1 || got[0].heard < 35000 || !got[0].counted {
		t.Fatalf("counted = %+v", got)
	}
	// After the 2-minute window: a new row.
	exec(t, pool, `UPDATE listener_plays SET last_heard_at = now() - interval '3 minutes'`)
	if err := st.RecordListenerProgress(ctx, l.ID, "long", 1000, 200000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, l.ID); len(got) != 2 || got[1].heard != 1000 || got[1].counted {
		t.Fatalf("new row = %+v", got)
	}
	// A short song counts at half its length (20 s of 40 s).
	if err := st.RecordListenerProgress(ctx, l.ID, "short", 20000, 40000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, l.ID); len(got) != 3 || !got[2].counted {
		t.Fatalf("short = %+v", got)
	}
	// Unknown track or listener: ignored.
	if err := st.RecordListenerProgress(ctx, l.ID, "missing", 20000, 0); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordListenerProgress(ctx, 999999, "long", 20000, 0); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, l.ID); len(got) != 3 {
		t.Fatalf("ignored reports wrote rows: %+v", got)
	}

	// A late flush for a song no longer playing: it extends a play heard in
	// the last 30 s (same clamp, count rule from the catalog length) and
	// never creates one.
	late, _ := st.CreateListener(ctx, store.NewListener{Email: "cy@example.com", InvitedBy: "a"})
	if err := st.ExtendListenerProgress(ctx, late.ID, "short", 15000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, late.ID); len(got) != 0 {
		t.Fatalf("late flush created a play: %+v", got)
	}
	if err := st.RecordListenerProgress(ctx, late.ID, "short", 15000, 40000); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `UPDATE listener_plays SET last_heard_at = now() - interval '10 seconds' WHERE listener_id = $1`, late.ID)
	if err := st.ExtendListenerProgress(ctx, late.ID, "short", 15000); err != nil {
		t.Fatal(err)
	}
	// 15 s + min(15 s, 10 s + 5 s) = 30 s, past half of the 40 s song.
	if got := plays(t, pool, late.ID); len(got) != 1 || got[0].heard < 29900 || got[0].heard > 30100 || !got[0].counted {
		t.Fatalf("late extend = %+v", got)
	}
	exec(t, pool, `UPDATE listener_plays SET last_heard_at = now() - interval '40 seconds' WHERE listener_id = $1`, late.ID)
	if err := st.ExtendListenerProgress(ctx, late.ID, "short", 15000); err != nil {
		t.Fatal(err)
	}
	if got := plays(t, pool, late.ID); len(got) != 1 || got[0].heard > 30100 {
		t.Fatalf("stale late flush extended: %+v", got)
	}

	// Concurrent reports for a new play: one row, no double count.
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := st.RecordListenerProgress(ctx, other.ID, "long", 10000, 200000); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := plays(t, pool, other.ID); len(got) != 1 || got[0].heard > 10000+4*5100 {
		t.Fatalf("concurrent = %+v", got)
	}

	// History: counted rows only, newest first, own rows only, paged.
	h, next, err := st.ListenerHistory(ctx, l.ID, 1, "")
	if err != nil || len(h) != 1 || next == nil || h[0].Track.ID != "short" {
		t.Fatalf("page 1 = %+v %v %v", h, next, err)
	}
	h, next2, err := st.ListenerHistory(ctx, l.ID, 1, *next)
	if err != nil || len(h) != 1 || next2 == nil || h[0].Track.ID != "long" {
		t.Fatalf("page 2 = %+v %v %v", h, next2, err)
	}
	tr := h[0].Track
	if tr.Name != "Long" || tr.AlbumName != "Album" || tr.CoverImageURL != "https://img.example.com/c.jpg" ||
		len(tr.Artists) != 1 || tr.Artists[0].Name != "Artist" || h[0].HeardMS < 35000 {
		t.Errorf("play = %+v", h[0])
	}
	if h, next3, err := st.ListenerHistory(ctx, l.ID, 1, *next2); err != nil || len(h) != 0 || next3 != nil {
		t.Errorf("page 3 = %+v %v %v", h, next3, err)
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
