package grpcapi

import (
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestPlaybackRoundTripMetadata(t *testing.T) {
	at := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	for name, in := range map[string]domain.Playback{
		"set": {
			Track: domain.Track{ID: "t1", Name: "Song", TrackNumber: proto.Int32(3), DiscNumber: proto.Int32(1)},
			Album: domain.Album{ID: "al1", Name: "Album", AlbumType: "single", TotalTracks: proto.Int32(2),
				ReleaseDatePrecision: "year"},
			Artists: []domain.Artist{{ID: "a1", Name: "A"}}, PlayedAt: at, FetchedAt: at,
		},
		"unset": {
			Track: domain.Track{ID: "t1", Name: "Song"}, Album: domain.Album{ID: "al1", Name: "Album"},
			Artists: []domain.Artist{{ID: "a1", Name: "A"}}, PlayedAt: at, FetchedAt: at,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// Serialize to make sure the values survive the wire.
			b, err := proto.Marshal(PlaybackToProto(in))
			if err != nil {
				t.Fatal(err)
			}
			var wire trackerv1.Playback
			if err := proto.Unmarshal(b, &wire); err != nil {
				t.Fatal(err)
			}
			if got := PlaybackFromProto(&wire); !reflect.DeepEqual(got, in) {
				t.Errorf("round trip:\n got %+v\nwant %+v", got, in)
			}
		})
	}
}

func TestPlaybackFromProtoNormalizes(t *testing.T) {
	got := PlaybackFromProto(&trackerv1.Playback{
		Track: &trackerv1.Track{Id: "t1", TrackNumber: proto.Int32(0), DiscNumber: proto.Int32(-2)},
		Album: &trackerv1.Album{Id: "al1", AlbumType: " Compilation ", TotalTracks: proto.Int32(-1),
			ReleaseDatePrecision: "week"},
	})
	if got.Track.TrackNumber != nil || got.Track.DiscNumber != nil || got.Album.TotalTracks != nil {
		t.Errorf("non-positive numbers kept: %+v %+v", got.Track, got.Album)
	}
	if got.Album.AlbumType != "compilation" || got.Album.ReleaseDatePrecision != "" {
		t.Errorf("album type %q, precision %q", got.Album.AlbumType, got.Album.ReleaseDatePrecision)
	}
	got = PlaybackFromProto(&trackerv1.Playback{Album: &trackerv1.Album{AlbumType: "appears_on"}})
	if got.Album.AlbumType != "" {
		t.Errorf("unknown album type kept: %q", got.Album.AlbumType)
	}
}
