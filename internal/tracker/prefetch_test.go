package tracker

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestUpNextFrom(t *testing.T) {
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.FixedZone("x", 3600))
	items := []domain.Upcoming{{
		Position: 0,
		CatalogEntry: domain.CatalogEntry{
			Track:   domain.Track{ID: "n1", Name: "Next", DurationMS: 1000},
			Album:   domain.Album{Name: "Album", CoverImageURL: "https://i/c"},
			Artists: []domain.Artist{{ID: "a1", Name: "A", URI: "spotify:artist:a1"}},
		},
		Prefetch: true,
	}}
	up := upNextFrom("cur", items, at)
	if up.ForTrackID != "cur" || !up.FetchedAt.Equal(at) || up.FetchedAt.Location() != time.UTC {
		t.Errorf("header = %+v", up)
	}
	if len(up.Items) != 1 {
		t.Fatalf("items = %+v", up.Items)
	}
	it := up.Items[0]
	if it.TrackID != "n1" || it.Name != "Next" || it.DurationMS != 1000 || it.AlbumName != "Album" ||
		it.CoverImageURL != "https://i/c" || len(it.Artists) != 1 || it.Artists[0] != (domain.UpNextName{ID: "a1", Name: "A"}) {
		t.Errorf("item = %+v", it)
	}
}

type fakeDeps struct {
	upserts  []string
	enrichOK bool
	current  string
	playing  bool
	upNext   []byte
	cleared  int
	seen     []string
	enriched []string
}

func (f *fakeDeps) UpsertEntry(_ context.Context, e domain.CatalogEntry, enrich bool) (bool, error) {
	f.upserts = append(f.upserts, e.Track.ID)
	return enrich && f.enrichOK, nil
}
func (f *fakeDeps) CurrentTrack(context.Context) (string, bool, error) {
	return f.current, f.playing, nil
}
func (f *fakeDeps) SetUpNext(_ context.Context, b []byte) error  { f.upNext = b; return nil }
func (f *fakeDeps) ClearUpNext(context.Context) error            { f.cleared++; return nil }
func (f *fakeDeps) MarkCatalogSeen(_ context.Context, id string) { f.seen = append(f.seen, id) }
func (f *fakeDeps) QueueEnrichment(_ context.Context, id, _ string) {
	f.enriched = append(f.enriched, id)
}

func up(id string, prefetch bool) domain.Upcoming {
	return domain.Upcoming{CatalogEntry: domain.CatalogEntry{Track: domain.Track{ID: id, Name: id}}, Prefetch: prefetch}
}

var discard = slog.New(slog.DiscardHandler)

func TestPrefetchUpcomingNeverMarksSeen(t *testing.T) {
	d := &fakeDeps{current: "cur", playing: true, enrichOK: true}
	var hooked []string
	ids, err := prefetchUpcoming(context.Background(), d, PrefetchOptions{}, discard,
		func(ids []string) { hooked = ids }, "cur",
		[]domain.Upcoming{up("a", true), up("cur", true), up("b", false), up("a", true), up("", true)}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []string{"a"}) || !slices.Equal(d.upserts, []string{"a"}) {
		t.Errorf("stored %v upserts %v", ids, d.upserts)
	}
	if len(d.seen) != 0 || len(d.enriched) != 0 {
		t.Errorf("catalog marked seen %v / enriched %v with enrichment off", d.seen, d.enriched)
	}
	if !slices.Equal(hooked, []string{"a", "b"}) {
		t.Errorf("hook = %v (deduped, current dropped, queue order)", hooked)
	}
	var list domain.UpNext
	if err := json.Unmarshal(d.upNext, &list); err != nil || len(list.Items) != 2 ||
		list.Items[0].TrackID != "a" || list.Items[1].Position != 1 {
		t.Errorf("up next = %s (%v)", d.upNext, err)
	}
}

func TestPrefetchUpcomingEnrich(t *testing.T) {
	d := &fakeDeps{current: "cur", playing: true, enrichOK: true}
	if _, err := prefetchUpcoming(context.Background(), d, PrefetchOptions{Enrich: true}, discard, nil, "cur",
		[]domain.Upcoming{up("a", true)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.seen, []string{"a"}) || !slices.Equal(d.enriched, []string{"a"}) {
		t.Errorf("seen %v enriched %v", d.seen, d.enriched)
	}
}

// A list read while playback stopped or moved on is not saved or acted on.
func TestPrefetchUpcomingStale(t *testing.T) {
	for name, d := range map[string]*fakeDeps{
		"stopped":   {current: "cur", playing: false},
		"moved on":  {current: "other", playing: true},
		"never set": {},
	} {
		called := false
		if _, err := prefetchUpcoming(context.Background(), d, PrefetchOptions{}, discard,
			func([]string) { called = true }, "cur", []domain.Upcoming{up("a", true)}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if d.upNext != nil || d.cleared != 0 || called {
			t.Errorf("%s: up next %s cleared %d hook %v", name, d.upNext, d.cleared, called)
		}
	}
}

// An empty queue while a song plays clears the list and tells the hook
// (nil), so the lookups and the cache warm-up drop the old upcoming ids.
func TestPrefetchUpcomingEmptyClears(t *testing.T) {
	for name, items := range map[string][]domain.Upcoming{
		"empty":        nil,
		"only current": {up("cur", true), up("", true)},
	} {
		d := &fakeDeps{current: "cur", playing: true}
		calls := 0
		var got []string
		if _, err := prefetchUpcoming(context.Background(), d, PrefetchOptions{}, discard,
			func(ids []string) { calls++; got = ids }, "cur", items, time.Now()); err != nil {
			t.Fatal(err)
		}
		if d.cleared != 1 || d.upNext != nil || calls != 1 || got != nil {
			t.Errorf("%s: cleared %d up next %s hook calls %d ids %v", name, d.cleared, d.upNext, calls, got)
		}
	}
}
