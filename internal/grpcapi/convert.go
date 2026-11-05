package grpcapi

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	trackerv1 "github.com/baoopn/spotify-tracker/gen/tracker/v1"
	"github.com/baoopn/spotify-tracker/internal/domain"
)

// PlaybackFromProto converts a wire playback to the domain type.
func PlaybackFromProto(p *trackerv1.Playback) domain.Playback {
	t, al := p.GetTrack(), p.GetAlbum()
	out := domain.Playback{
		Track: domain.Track{
			ID: t.GetId(), Name: t.GetName(), URI: t.GetUri(), ISRC: t.GetIsrc(),
			DurationMS: t.GetDurationMs(), Explicit: t.GetExplicit(), ExternalURL: t.GetExternalUrl(),
		},
		Album: domain.Album{
			ID: al.GetId(), Name: al.GetName(), ReleaseDate: al.GetReleaseDate(),
			CoverImageURL: al.GetCoverImageUrl(), URI: al.GetUri(), ExternalURL: al.GetExternalUrl(),
		},
		IsPlaying: p.GetIsPlaying(),
	}
	for _, a := range p.GetArtists() {
		if a.GetId() == "" {
			continue
		}
		out.Artists = append(out.Artists, domain.Artist{
			ID: a.GetId(), Name: a.GetName(), URI: a.GetUri(), ExternalURL: a.GetExternalUrl(),
		})
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
		Track: &trackerv1.Track{
			Id: p.Track.ID, Name: p.Track.Name, Uri: p.Track.URI, Isrc: p.Track.ISRC,
			DurationMs: p.Track.DurationMS, Explicit: p.Track.Explicit, ExternalUrl: p.Track.ExternalURL,
		},
		Album: &trackerv1.Album{
			Id: p.Album.ID, Name: p.Album.Name, ReleaseDate: p.Album.ReleaseDate,
			CoverImageUrl: p.Album.CoverImageURL, Uri: p.Album.URI, ExternalUrl: p.Album.ExternalURL,
		},
		IsPlaying: p.IsPlaying,
		PlayedAt:  timestamppb.New(p.PlayedAt),
		FetchedAt: timestamppb.New(p.FetchedAt),
	}
	for _, a := range p.Artists {
		out.Artists = append(out.Artists, &trackerv1.Artist{
			Id: a.ID, Name: a.Name, Uri: a.URI, ExternalUrl: a.ExternalURL,
		})
	}
	if p.ProgressMS != nil {
		out.ProgressMs = proto.Int32(*p.ProgressMS)
	}
	if p.Context != nil {
		out.Context = &trackerv1.PlaybackContext{Type: p.Context.Type, Uri: p.Context.URI, ExternalUrl: p.Context.ExternalURL}
	}
	return out
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
