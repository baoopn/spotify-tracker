package grpcapi

import (
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestUpcomingRoundTrip(t *testing.T) {
	in := domain.Upcoming{
		Position: 1,
		CatalogEntry: domain.CatalogEntry{
			Track: domain.Track{ID: "t1", Name: "Song", ISRC: "X", DurationMS: 1000, AlbumID: "al1",
				TrackNumber: proto.Int32(2)},
			Album: domain.Album{ID: "al1", Name: "Album", AlbumType: "album", CoverImageURL: "https://i/c",
				TotalTracks: proto.Int32(10), ReleaseDatePrecision: "day"},
			Artists: []domain.Artist{{ID: "a1", Name: "A"}, {ID: "a2", Name: "B"}},
		},
		Prefetch: true,
	}
	b, err := proto.Marshal(UpcomingToProto(in))
	if err != nil {
		t.Fatal(err)
	}
	var wire trackerv1.UpcomingTrack
	if err := proto.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if got := UpcomingFromProto(&wire); !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, in)
	}
}

func TestUpcomingFromRequest(t *testing.T) {
	ok := &trackerv1.UpcomingTrack{Track: &trackerv1.Track{Id: "t1", Name: "Song"}, Prefetch: true}
	req := &trackerv1.PrefetchUpcomingRequest{Tracks: []*trackerv1.UpcomingTrack{
		ok,
		{Track: &trackerv1.Track{Id: "t2"}},      // no name
		{Track: &trackerv1.Track{Name: "No id"}}, // no id
		{},
	}}
	items, skipped, err := upcomingFromRequest(req)
	if err != nil || skipped != 3 || len(items) != 1 || items[0].Track.ID != "t1" || !items[0].Prefetch {
		t.Fatalf("items %+v skipped %d err %v", items, skipped, err)
	}

	big := &trackerv1.PrefetchUpcomingRequest{}
	for range maxUpcoming + 1 {
		big.Tracks = append(big.Tracks, ok)
	}
	if _, _, err := upcomingFromRequest(big); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v", err)
	}
}
