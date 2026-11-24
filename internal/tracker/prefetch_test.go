package tracker

import (
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
