package spotify

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	qTrackA = "0000000000000000000001"
	qTrackB = "0000000000000000000002"
	qTrackC = "0000000000000000000003"
)

const queueBody = `{
  "currently_playing": {"id": "playingNowIgnored00000", "name": "Now"},
  "queue": [
    {"type": "episode", "id": "0000000000000000000009", "name": "A podcast"},
    {"type": "track", "id": "` + qTrackA + `", "name": "Next", "duration_ms": 180000,
     "external_ids": {"isrc": "USRC1"},
     "album": {"id": "alb1", "name": "Album", "album_type": "single", "total_tracks": 1,
       "images": [{"url": "https://i.scdn.co/image/a"}]},
     "artists": [{"id": "art1", "name": "Artist"}, {"id": "", "name": "No id"}]},
    {"type": "track", "is_local": true, "id": null, "name": "Local file"},
    {"type": "track", "id": "short", "name": "Bad id"},
    {"type": "track", "id": "` + qTrackA + `", "name": "Next again"},
    {"type": "track", "id": "` + qTrackB + `", "name": ""},
    {"type": "track", "id": "` + qTrackC + `", "name": "Third"}
  ]
}`

func TestQueueFiltersAndMaps(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me/player/queue" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(queueBody))
	})
	got, err := p.Queue(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Track.ID != qTrackA || got[1].Track.ID != qTrackC {
		t.Fatalf("got %+v", got)
	}
	a := got[0]
	if a.Track.Name != "Next" || a.Track.ISRC != "USRC1" || a.Track.AlbumID != "alb1" ||
		a.Album.CoverImageURL != "https://i.scdn.co/image/a" || a.Album.AlbumType != "single" {
		t.Errorf("mapping: %+v", a)
	}
	if len(a.Artists) != 1 || a.Artists[0].ID != "art1" {
		t.Errorf("artists: %+v", a.Artists)
	}
}

func TestQueueLimitCountsTracks(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(queueBody)) })
	got, err := p.Queue(context.Background(), 1)
	if err != nil || len(got) != 1 || got[0].Track.ID != qTrackA {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestQueueEmpty(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	got, err := p.Queue(context.Background(), 3)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestQueueRefreshesOn401(t *testing.T) {
	p, tokens := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"queue": []}`))
	})
	if _, err := p.Queue(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if tokens.forced.Load() != 1 {
		t.Errorf("forced = %d", tokens.forced.Load())
	}
}

func TestQueueRateLimited(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := p.Queue(context.Background(), 1)
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("err = %v", err)
	}
}

func TestQueueForbidden(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	if _, err := p.Queue(context.Background(), 1); !errors.Is(err, ErrQueueForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestQueueBadBody(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"queue": [`)) })
	if _, err := p.Queue(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "decode queue") {
		t.Fatalf("err = %v", err)
	}
	p, _ = playerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream"))
	})
	if _, err := p.Queue(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
}
