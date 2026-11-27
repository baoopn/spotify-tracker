package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

func TestPGTrackAudio(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `TRUNCATE track_audio`)
	exec(t, pool, `INSERT INTO artists (id, name) VALUES ('ta_ar', 'Singer') ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`)
	exec(t, pool, `INSERT INTO albums (id, name, cover_image_url) VALUES ('ta_al', 'Record', 'https://img.example.com/r.jpg')
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, cover_image_url = EXCLUDED.cover_image_url`)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms, album_id) VALUES ('a1', 'Alpha Song', 200000, 'ta_al'),
		('a2', 'Beta', 100000, NULL), ('a3', 'Gamma', 100000, NULL)`)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id, position) VALUES ('a1', 'ta_ar', 0), ('a2', 'ta_ar', 0)`)
	exec(t, pool, `INSERT INTO listening_history (track_id, played_at, updated_at, duration_ms, replay_count)
		VALUES ('a1', now() - interval '1 hour', now(), 1000, 2), ('a2', now() - interval '2 hours', now(), 1000, 5),
		       ('a3', now() - interval '3 hours', now(), 1000, 1)`)

	mp3 := "audio/mpeg"
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	added, removed, err := st.ReplaceTrackAudio(ctx, []store.TrackAudio{
		{TrackID: "a1", Ext: "mp3", SizeBytes: 100, ContentType: &mp3, LastModified: &old},
		{TrackID: "notcatalog", Ext: "mp3", SizeBytes: 50, LastModified: &newer},
	})
	if err != nil || len(added) != 2 || len(removed) != 0 {
		t.Fatalf("first sync = %v %v %v", added, removed, err)
	}
	// Second listing: a1 changed, notcatalog gone, a2 new.
	added, removed, err = st.ReplaceTrackAudio(ctx, []store.TrackAudio{
		{TrackID: "a1", Ext: "flac", SizeBytes: 300, LastModified: &old},
		{TrackID: "a2", Ext: "m4a", SizeBytes: 200, LastModified: &newer},
	})
	if err != nil || !slices.Equal(added, []string{"a2"}) || !slices.Equal(removed, []string{"notcatalog"}) {
		t.Fatalf("second sync = %v %v %v", added, removed, err)
	}
	a, err := st.TrackAudioFor(ctx, "a1")
	if err != nil || a.Ext != "flac" || a.SizeBytes != 300 || a.ContentType != nil {
		t.Errorf("a1 = %+v %v", a, err)
	}
	if _, err := st.TrackAudioFor(ctx, "notcatalog"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed err = %v", err)
	}
	// A single upsert (from a HEAD lookup).
	if err := st.UpsertTrackAudio(ctx, store.TrackAudio{TrackID: "zz", Ext: "mp3", SizeBytes: 7}); err != nil {
		t.Fatal(err)
	}

	files, total, err := st.AudioFiles(ctx, "", 50, 0)
	if err != nil || total != 3 || len(files) != 3 {
		t.Fatalf("files = %+v %d %v", files, total, err)
	}
	// Newest last_modified first, unknown last.
	if files[0].TrackID != "a2" || files[1].TrackID != "a1" || files[2].TrackID != "zz" || files[2].Track != nil {
		t.Errorf("order = %s %s %s", files[0].TrackID, files[1].TrackID, files[2].TrackID)
	}
	if f := files[1]; f.Track == nil || f.Track.Name != "Alpha Song" || f.Track.AlbumName != "Record" ||
		len(f.Track.Artists) != 1 || f.OwnerPlays != 2 {
		t.Errorf("a1 row = %+v %+v", f, f.Track)
	}
	if got, n, _ := st.AudioFiles(ctx, "alpha", 50, 0); n != 1 || got[0].TrackID != "a1" {
		t.Errorf("search name = %+v", got)
	}
	if _, n, _ := st.AudioFiles(ctx, "singer", 50, 0); n != 2 {
		t.Errorf("search artist = %d", n)
	}
	sum, err := st.AudioSummary(ctx)
	if err != nil || sum.Files != 3 || sum.TotalBytes != 507 || sum.TracksInCatalog != 2 {
		t.Errorf("summary = %+v %v", sum, err)
	}

	missing, n, err := st.MissingAudio(ctx, 50, 0)
	if err != nil || n != 1 || len(missing) != 1 || missing[0].Track.ID != "a3" || missing[0].OwnerPlays != 1 {
		t.Errorf("missing = %+v %d %v", missing, n, err)
	}

	// The tracks filter and has_audio.
	has, n, err := st.AdminTrackList(ctx, store.TrackQuery{Audio: store.AudioFilterHas, Sort: "name", Order: "asc", Limit: 50})
	if err != nil || n != 2 || !has[0].HasAudio || has[0].Track.ID != "a1" {
		t.Errorf("has audio = %+v %d %v", has, n, err)
	}
	none, n, _ := st.AdminTrackList(ctx, store.TrackQuery{Audio: store.AudioFilterMissing, Limit: 50})
	if n != 1 || none[0].Track.ID != "a3" || none[0].HasAudio {
		t.Errorf("missing audio = %+v %d", none, n)
	}

	// An empty complete listing removes every row.
	if _, removed, err := st.ReplaceTrackAudio(ctx, nil); err != nil || len(removed) != 3 {
		t.Errorf("empty listing removed %v %v", removed, err)
	}
}
