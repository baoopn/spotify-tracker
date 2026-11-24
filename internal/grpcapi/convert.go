package grpcapi

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
)

// PlaybackFromProto converts a wire playback to the domain type. Unknown
// album types and precisions become "", and numbers <= 0 become nil.
func PlaybackFromProto(p *trackerv1.Playback) domain.Playback {
	out := domain.Playback{
		Track:     trackFromProto(p.GetTrack()),
		Album:     albumFromProto(p.GetAlbum()),
		Artists:   artistsFromProto(p.GetArtists()),
		IsPlaying: p.GetIsPlaying(),
	}
	if p.ProgressMs != nil {
		v := p.GetProgressMs()
		out.ProgressMS = &v
	}
	if p.GetPlayedAt() != nil {
		out.PlayedAt = p.GetPlayedAt().AsTime()
	}
	if p.GetFetchedAt() != nil {
		out.FetchedAt = p.GetFetchedAt().AsTime()
	}
	if c := p.GetContext(); c.GetUri() != "" {
		out.Context = &domain.PlaybackContext{Type: c.GetType(), URI: c.GetUri(), ExternalURL: c.GetExternalUrl()}
	}
	return out
}

// PlaybackToProto converts a domain playback to the wire type.
func PlaybackToProto(p domain.Playback) *trackerv1.Playback {
	out := &trackerv1.Playback{
		Track:     trackToProto(p.Track),
		Album:     albumToProto(p.Album),
		Artists:   artistsToProto(p.Artists),
		IsPlaying: p.IsPlaying,
		PlayedAt:  timestamppb.New(p.PlayedAt),
		FetchedAt: timestamppb.New(p.FetchedAt),
	}
	if p.ProgressMS != nil {
		out.ProgressMs = proto.Int32(*p.ProgressMS)
	}
	if p.Context != nil {
		out.Context = &trackerv1.PlaybackContext{Type: p.Context.Type, Uri: p.Context.URI, ExternalUrl: p.Context.ExternalURL}
	}
	return out
}

func trackFromProto(t *trackerv1.Track) domain.Track {
	return domain.Track{
		ID: t.GetId(), Name: t.GetName(), URI: t.GetUri(), ISRC: t.GetIsrc(),
		DurationMS: t.GetDurationMs(), Explicit: t.GetExplicit(), ExternalURL: t.GetExternalUrl(),
		TrackNumber: domain.PositiveInt32(t.GetTrackNumber()), DiscNumber: domain.PositiveInt32(t.GetDiscNumber()),
	}
}

func albumFromProto(al *trackerv1.Album) domain.Album {
	return domain.Album{
		ID: al.GetId(), Name: al.GetName(), ReleaseDate: al.GetReleaseDate(),
		CoverImageURL: al.GetCoverImageUrl(), URI: al.GetUri(), ExternalURL: al.GetExternalUrl(),
		AlbumType:            domain.NormalizeAlbumType(al.GetAlbumType()),
		TotalTracks:          domain.PositiveInt32(al.GetTotalTracks()),
		ReleaseDatePrecision: domain.NormalizeReleaseDatePrecision(al.GetReleaseDatePrecision()),
	}
}

// artistsFromProto keeps credit order and drops artists without an id.
func artistsFromProto(in []*trackerv1.Artist) []domain.Artist {
	var out []domain.Artist
	for _, a := range in {
		if a.GetId() == "" {
			continue
		}
		out = append(out, domain.Artist{
			ID: a.GetId(), Name: a.GetName(), URI: a.GetUri(), ExternalURL: a.GetExternalUrl(),
		})
	}
	return out
}

func trackToProto(t domain.Track) *trackerv1.Track {
	return &trackerv1.Track{
		Id: t.ID, Name: t.Name, Uri: t.URI, Isrc: t.ISRC,
		DurationMs: t.DurationMS, Explicit: t.Explicit, ExternalUrl: t.ExternalURL,
		TrackNumber: copyInt32(t.TrackNumber), DiscNumber: copyInt32(t.DiscNumber),
	}
}

func albumToProto(a domain.Album) *trackerv1.Album {
	return &trackerv1.Album{
		Id: a.ID, Name: a.Name, ReleaseDate: a.ReleaseDate,
		CoverImageUrl: a.CoverImageURL, Uri: a.URI, ExternalUrl: a.ExternalURL,
		AlbumType: a.AlbumType, TotalTracks: copyInt32(a.TotalTracks),
		ReleaseDatePrecision: a.ReleaseDatePrecision,
	}
}

func artistsToProto(in []domain.Artist) []*trackerv1.Artist {
	out := make([]*trackerv1.Artist, 0, len(in))
	for _, a := range in {
		out = append(out, &trackerv1.Artist{Id: a.ID, Name: a.Name, Uri: a.URI, ExternalUrl: a.ExternalURL})
	}
	return out
}

// UpcomingFromProto converts a wire queue entry to the domain type.
func UpcomingFromProto(u *trackerv1.UpcomingTrack) domain.Upcoming {
	t := trackFromProto(u.GetTrack())
	al := albumFromProto(u.GetAlbum())
	t.AlbumID = al.ID
	return domain.Upcoming{
		Position:     u.GetPosition(),
		CatalogEntry: domain.CatalogEntry{Track: t, Album: al, Artists: artistsFromProto(u.GetArtists())},
		Prefetch:     u.GetPrefetch(),
	}
}

// UpcomingToProto converts a domain queue entry to the wire type.
func UpcomingToProto(u domain.Upcoming) *trackerv1.UpcomingTrack {
	return &trackerv1.UpcomingTrack{
		Position: u.Position, Track: trackToProto(u.Track), Album: albumToProto(u.Album),
		Artists: artistsToProto(u.Artists), Prefetch: u.Prefetch,
	}
}

func copyInt32(v *int32) *int32 {
	if v == nil {
		return nil
	}
	return proto.Int32(*v)
}

// FeaturesFromProto converts wire audio features to the domain type.
func FeaturesFromProto(f *trackerv1.AudioFeatures) domain.AudioFeatures {
	if f == nil {
		return domain.AudioFeatures{}
	}
	return domain.AudioFeatures{
		Acousticness: f.Acousticness, Danceability: f.Danceability, Energy: f.Energy,
		Instrumentalness: f.Instrumentalness, Key: f.Key, Liveness: f.Liveness,
		Loudness: f.Loudness, Mode: f.Mode, Speechiness: f.Speechiness, Tempo: f.Tempo,
		TimeSignature: f.TimeSignature, Valence: f.Valence,
	}
}

// FeaturesToProto converts domain audio features to the wire type.
func FeaturesToProto(f domain.AudioFeatures) *trackerv1.AudioFeatures {
	return &trackerv1.AudioFeatures{
		Acousticness: f.Acousticness, Danceability: f.Danceability, Energy: f.Energy,
		Instrumentalness: f.Instrumentalness, Key: f.Key, Liveness: f.Liveness,
		Loudness: f.Loudness, Mode: f.Mode, Speechiness: f.Speechiness, Tempo: f.Tempo,
		TimeSignature: f.TimeSignature, Valence: f.Valence,
	}
}
