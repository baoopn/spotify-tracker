package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const playingBody = `{
  "timestamp": 1762300000000,
  "is_playing": true,
  "progress_ms": 42000,
  "currently_playing_type": "track",
  "context": {"type": "playlist", "uri": "spotify:playlist:p1", "external_urls": {"spotify": "https://open.spotify.com/playlist/p1"}},
  "item": {
    "id": "trk1", "name": "Song", "uri": "spotify:track:trk1", "duration_ms": 200000,
    "explicit": true,
    "external_ids": {"isrc": "USRC17607839"},
    "external_urls": {"spotify": "https://open.spotify.com/track/trk1"},
    "album": {"id": "alb1", "name": "Album", "release_date": "2020-01-01",
      "images": [{"url": "https://i.scdn.co/image/big"}, {"url": "https://i.scdn.co/image/small"}]},
    "artists": [{"id": "art1", "name": "Artist One"}, {"id": "art2", "name": "Artist Two"}]
  }
}`

// staticTokens is a TokenSource that counts forced refreshes.
type staticTokens struct{ forced atomic.Int32 }

func (s *staticTokens) AccessToken(_ context.Context, force bool) (string, error) {
	if force {
		s.forced.Add(1)
		return "fresh", nil
	}
	return "at", nil
}

func playerServer(t *testing.T, handler http.HandlerFunc) (*Player, *staticTokens) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	tokens := &staticTokens{}
	return NewPlayer(tokens, srv.URL+"/v1", nil), tokens
}

func TestCurrentlyPlayingMapsTrack(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/player/currently-playing" || r.Header.Get("Authorization") != "Bearer at" {
			t.Errorf("unexpected request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(playingBody))
	})
	pb, err := p.CurrentlyPlaying(context.Background())
	if err != nil || pb == nil {
		t.Fatalf("got %v, %v", pb, err)
	}
	if pb.Track.ID != "trk1" || pb.Track.ISRC != "USRC17607839" || !pb.Track.Explicit || pb.Track.DurationMS != 200000 {
		t.Errorf("track mapped wrong: %+v", pb.Track)
	}
	if pb.Album.CoverImageURL != "https://i.scdn.co/image/big" || pb.Track.AlbumID != "alb1" {
		t.Errorf("album mapped wrong: %+v", pb.Album)
	}
	if len(pb.Artists) != 2 || pb.Artists[1].Name != "Artist Two" {
		t.Errorf("artists mapped wrong: %+v", pb.Artists)
	}
	if pb.ProgressMS == nil || *pb.ProgressMS != 42000 || !pb.IsPlaying {
		t.Errorf("progress mapped wrong")
	}
	if pb.Context == nil || pb.Context.Type != "playlist" || pb.Context.ID() != "p1" {
		t.Errorf("context mapped wrong: %+v", pb.Context)
	}
	if !pb.PlayedAt.Equal(time.UnixMilli(1762300000000)) {
		t.Errorf("played_at = %v", pb.PlayedAt)
	}
}

func TestCurrentlyPlayingRefreshesOn401(t *testing.T) {
	p, tokens := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	pb, err := p.CurrentlyPlaying(context.Background())
	if err != nil || pb != nil {
		t.Fatalf("got %v, %v", pb, err)
	}
	if tokens.forced.Load() != 1 {
		t.Errorf("forced refreshes = %d, want 1", tokens.forced.Load())
	}
}

func TestCurrentlyPlayingPodcastIgnored(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"is_playing":true,"currently_playing_type":"episode","item":{"id":"ep"}}`))
	})
	pb, err := p.CurrentlyPlaying(context.Background())
	if err != nil || pb != nil {
		t.Fatalf("got %v, %v; want nil, nil", pb, err)
	}
}

func TestRateLimited(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"status":429,"reason":"QUOTA_EXCEEDED"}}`))
	})
	_, err := p.CurrentlyPlaying(context.Background())
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 17*time.Second || rl.Reason != "QUOTA_EXCEEDED" {
		t.Fatalf("got %v", err)
	}
}

func oauthServer(t *testing.T, code int, body string, check func(url.Values)) *OAuth {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "id" || p != "secret" {
			t.Errorf("bad basic auth")
		}
		_ = r.ParseForm()
		if check != nil {
			check(r.Form)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &OAuth{ClientID: "id", ClientSecret: "secret", AccountsBase: srv.URL}
}

func TestRefresh(t *testing.T) {
	o := oauthServer(t, 200, `{"access_token":"at","expires_in":3600,"scope":"user-read-currently-playing user-read-recently-played","refresh_token":"rotated"}`,
		func(f url.Values) {
			if f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "rt" {
				t.Errorf("bad form %v", f)
			}
		})
	tok, err := o.Refresh(context.Background(), "rt")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at" || tok.RefreshToken != "rotated" || len(tok.Scopes) != 2 || time.Until(tok.ExpiresAt) < 59*time.Minute {
		t.Errorf("got %+v", tok)
	}
}

func TestRefreshInvalidGrant(t *testing.T) {
	o := oauthServer(t, 400, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`, nil)
	if _, err := o.Refresh(context.Background(), "rt"); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("want ErrInvalidGrant, got %v", err)
	}
	if _, err := o.Refresh(context.Background(), ""); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
}

func TestMissingScope(t *testing.T) {
	o := oauthServer(t, 200, `{"access_token":"at","expires_in":3600,"scope":"user-top-read"}`, nil)
	if _, err := o.Refresh(context.Background(), "rt"); !errors.Is(err, ErrMissingScope) {
		t.Fatalf("want ErrMissingScope, got %v", err)
	}
}

func TestExchangeAndAuthorizeURL(t *testing.T) {
	o := oauthServer(t, 200, `{"access_token":"at","expires_in":3600,"scope":"user-read-currently-playing","refresh_token":"new"}`,
		func(f url.Values) {
			if f.Get("grant_type") != "authorization_code" || f.Get("code") != "c0de" || f.Get("redirect_uri") != "http://127.0.0.1:8080/cb" {
				t.Errorf("bad form %v", f)
			}
		})
	tok, err := o.Exchange(context.Background(), "c0de", "http://127.0.0.1:8080/cb")
	if err != nil || tok.RefreshToken != "new" {
		t.Fatalf("got %+v, %v", tok, err)
	}
	u, err := url.Parse(o.AuthorizeURL("st4te", "http://127.0.0.1:8080/cb"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "st4te" || q.Get("response_type") != "code" || !strings.Contains(q.Get("scope"), RequiredScope) {
		t.Errorf("bad authorize url %s", u)
	}
}
