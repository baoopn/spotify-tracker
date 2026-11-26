package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	pcTrack  = "4uLU6hMCjMI75M1A2tKUQC"
	pcDevice = "dev-1"
)

// recorded is one request seen by the fake Web API.
type recorded struct {
	method, path, query, body string
}

// commandServer answers every request with status and body, recording it.
func commandServer(t *testing.T, status int, body string) (*Player, *[]recorded) {
	t.Helper()
	var seen []recorded
	p, _ := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, recorded{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		if r.Header.Get("Authorization") == "" {
			t.Error("no Authorization header")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	return p, &seen
}

func intp(n int) *int     { return &n }
func i64p(n int64) *int64 { return &n }

func TestPlayerCommandsSendRequests(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name                string
		run                 func(*Player) error
		method, path, query string
		body                string // JSON, compared semantically; "" = none
	}{
		{"pause", func(p *Player) error { return p.Pause(ctx, "") }, "PUT", "/v1/me/player/pause", "", ""},
		{"pause on device", func(p *Player) error { return p.Pause(ctx, pcDevice) }, "PUT", "/v1/me/player/pause", "device_id=dev-1", ""},
		{"next", func(p *Player) error { return p.Next(ctx, "") }, "POST", "/v1/me/player/next", "", ""},
		{"previous", func(p *Player) error { return p.Previous(ctx, "") }, "POST", "/v1/me/player/previous", "", ""},
		{"seek", func(p *Player) error { return p.Seek(ctx, 61000, "") }, "PUT", "/v1/me/player/seek", "position_ms=61000", ""},
		{"volume", func(p *Player) error { return p.SetVolume(ctx, 40, pcDevice) }, "PUT", "/v1/me/player/volume", "device_id=dev-1&volume_percent=40", ""},
		{"shuffle", func(p *Player) error { return p.SetShuffle(ctx, true, "") }, "PUT", "/v1/me/player/shuffle", "state=true", ""},
		{"repeat", func(p *Player) error { return p.SetRepeat(ctx, RepeatContext, "") }, "PUT", "/v1/me/player/repeat", "state=context", ""},
		{"transfer", func(p *Player) error { return p.Transfer(ctx, pcDevice, true) }, "PUT", "/v1/me/player", "", `{"device_ids":["dev-1"],"play":true}`},
		{"queue", func(p *Player) error { return p.AddToQueue(ctx, pcTrack, "") }, "POST", "/v1/me/player/queue", "uri=spotify%3Atrack%3A" + pcTrack, ""},
		{"resume", func(p *Player) error { return p.Play(ctx, PlayOptions{}) }, "PUT", "/v1/me/player/play", "", ""},
		{"play uris", func(p *Player) error {
			return p.Play(ctx, PlayOptions{DeviceID: pcDevice, URIs: []string{pcTrack}, PositionMS: i64p(5)})
		}, "PUT", "/v1/me/player/play", "device_id=dev-1", `{"uris":["spotify:track:` + pcTrack + `"],"position_ms":5}`},
		{"play context", func(p *Player) error {
			return p.Play(ctx, PlayOptions{ContextURI: "spotify:album:" + pcTrack, OffsetPosition: intp(2)})
		}, "PUT", "/v1/me/player/play", "", `{"context_uri":"spotify:album:` + pcTrack + `","offset":{"position":2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, seen := commandServer(t, http.StatusNoContent, "")
			if err := tc.run(p); err != nil {
				t.Fatal(err)
			}
			if len(*seen) != 1 {
				t.Fatalf("%d requests", len(*seen))
			}
			got := (*seen)[0]
			if got.method != tc.method || got.path != tc.path || got.query != tc.query {
				t.Errorf("got %s %s?%s, want %s %s?%s", got.method, got.path, got.query, tc.method, tc.path, tc.query)
			}
			if !jsonEqual(got.body, tc.body) {
				t.Errorf("body %q, want %q", got.body, tc.body)
			}
		})
	}
}

func jsonEqual(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func TestPlayerCommandsValidate(t *testing.T) {
	ctx := context.Background()
	p, seen := commandServer(t, http.StatusNoContent, "")
	for name, err := range map[string]error{
		"volume high":    p.SetVolume(ctx, 101, ""),
		"volume low":     p.SetVolume(ctx, -1, ""),
		"repeat":         p.SetRepeat(ctx, "all", ""),
		"seek":           p.Seek(ctx, -1, ""),
		"transfer empty": p.Transfer(ctx, "", false),
		"device spaces":  p.Pause(ctx, "a b"),
		"queue bad id":   p.AddToQueue(ctx, "nope", ""),
		"queue episode":  p.AddToQueue(ctx, "spotify:episode:"+pcTrack, ""),
		"play both":      p.Play(ctx, PlayOptions{ContextURI: "spotify:album:" + pcTrack, URIs: []string{pcTrack}}),
		"play bad ctx":   p.Play(ctx, PlayOptions{ContextURI: "spotify:track:" + pcTrack}),
		"offset alone":   p.Play(ctx, PlayOptions{OffsetPosition: intp(1)}),
		"search empty":   func() error { _, err := p.SearchTracks(ctx, "  ", 5); return err }(),
		"search limit":   func() error { _, err := p.SearchTracks(ctx, "x", 11); return err }(),
	} {
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: got %v, want ErrInvalidArgument", name, err)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("invalid calls sent %d requests", len(*seen))
	}
}

func TestPlayerErrorMapping(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{404, `{"error":{"status":404,"message":"Player command failed: No active device found","reason":"NO_ACTIVE_DEVICE"}}`, ErrNoActiveDevice},
		{404, `{"error":{"status":404,"message":"Device not found"}}`, ErrNotFound},
		{403, `{"error":{"status":403,"message":"Player command failed: Premium required","reason":"PREMIUM_REQUIRED"}}`, ErrPremiumRequired},
		{403, `{"error":{"status":403,"message":"Player command failed: Restriction violated","reason":"UNKNOWN"}}`, ErrRestricted},
	} {
		p, _ := commandServer(t, tc.status, tc.body)
		if err := p.Next(ctx, ""); !errors.Is(err, tc.want) {
			t.Errorf("%d %s: got %v, want %v", tc.status, tc.body, err, tc.want)
		}
	}

	p, _ := commandServer(t, http.StatusBadGateway, `{"error":{"status":502,"message":"Bad gateway"}}`)
	var se *StatusError
	if err := p.Pause(ctx, ""); !errors.As(err, &se) || se.Code != 502 || se.Permanent() {
		t.Errorf("502: got %v", err)
	}
}

func TestPlayerHonors429(t *testing.T) {
	p, _ := playerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var rl *RateLimitError
	if err := p.Next(context.Background(), ""); !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("got %v", err)
	}
}

func TestPlayerRefreshesOn401(t *testing.T) {
	calls := 0
	p, tokens := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := p.Pause(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || tokens.forced.Load() != 1 {
		t.Errorf("calls %d, forced %d", calls, tokens.forced.Load())
	}
}

const stateBody = `{
  "device": {"id": "dev-1", "name": "Desk", "type": "Computer", "volume_percent": 55,
    "supports_volume": true, "is_active": true, "is_restricted": false},
  "shuffle_state": true, "repeat_state": "track", "timestamp": 1, "progress_ms": 42000,
  "is_playing": true, "currently_playing_type": "track",
  "context": {"type": "album", "uri": "spotify:album:alb1"},
  "actions": {"disallows": {"resuming": true, "pausing": false}},
  "item": {"id": "` + pcTrack + `", "name": "Song", "uri": "spotify:track:` + pcTrack + `", "duration_ms": 200000,
    "album": {"name": "Album", "images": [{"url": "https://i.scdn.co/big"}, {"url": "https://i.scdn.co/mid"}]},
    "artists": [{"id": "a1", "name": "One"}, {"id": "a2", "name": "Two"}]}
}`

func TestPlaybackState(t *testing.T) {
	p, seen := commandServer(t, http.StatusOK, stateBody)
	st, err := p.PlaybackState(context.Background())
	if err != nil || st == nil {
		t.Fatalf("got %v, %v", st, err)
	}
	if (*seen)[0].path != "/v1/me/player" || (*seen)[0].query != "additional_types=track" {
		t.Errorf("request %+v", (*seen)[0])
	}
	if !st.IsPlaying || st.ProgressMS != 42000 || !st.ShuffleState || st.RepeatState != "track" {
		t.Errorf("state %+v", st)
	}
	if d := st.Device; d == nil || d.ID != "dev-1" || d.VolumePercent == nil || *d.VolumePercent != 55 || !d.SupportsVolume {
		t.Errorf("device %+v", st.Device)
	}
	if it := st.Item; it == nil || it.Name != "Song" || it.CoverImageURL != "https://i.scdn.co/big" ||
		len(it.Artists) != 2 || it.DurationMS != 200000 {
		t.Errorf("item %+v", st.Item)
	}
	if !st.Disallows["resuming"] || st.Disallows["pausing"] || st.Context == nil {
		t.Errorf("disallows %+v context %+v", st.Disallows, st.Context)
	}

	p, _ = commandServer(t, http.StatusNoContent, "")
	if st, err := p.PlaybackState(context.Background()); st != nil || err != nil {
		t.Errorf("204: got %+v, %v", st, err)
	}

	p, _ = commandServer(t, http.StatusOK, `{"is_playing": true, "currently_playing_type": "episode",
		"device": {"id": "d"}, "item": {"id": "ep", "name": "Episode"}}`)
	if st, err := p.PlaybackState(context.Background()); err != nil || st.Item != nil || st.RepeatState != "off" {
		t.Errorf("episode: got %+v, %v", st, err)
	}
}

func TestDevicesSkipsDevicesWithoutID(t *testing.T) {
	p, _ := commandServer(t, http.StatusOK, `{"devices": [{"id": "d1", "name": "Phone", "type": "Smartphone"},
		{"id": null, "name": "Restricted"}]}`)
	got, err := p.Devices(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "d1" || got[0].Type != "Smartphone" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPlayerQueueKeepsOrderAndCurrent(t *testing.T) {
	p, _ := commandServer(t, http.StatusOK, queueBody)
	got, err := p.PlayerQueue(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentlyPlaying == nil || got.CurrentlyPlaying.Name != "Now" {
		t.Errorf("current %+v", got.CurrentlyPlaying)
	}
	// The episode, the local file without a URI and the bad id are skipped;
	// the repeat is kept.
	names := []string{}
	for _, q := range got.Queue {
		names = append(names, q.Name)
	}
	if strings.Join(names, "|") != "Next|Next again|Third" {
		t.Errorf("queue %v", names)
	}
	if got.Queue[0].URI != "spotify:track:"+qTrackA || got.Queue[0].CoverImageURL != "https://i.scdn.co/image/a" {
		t.Errorf("first %+v", got.Queue[0])
	}

	p, _ = commandServer(t, http.StatusNoContent, "")
	if got, err := p.PlayerQueue(context.Background(), 3); err != nil || got.CurrentlyPlaying != nil || len(got.Queue) != 0 {
		t.Errorf("204: got %+v, %v", got, err)
	}
}

func TestSearchTracks(t *testing.T) {
	p, seen := commandServer(t, http.StatusOK, `{"tracks": {"items": [
		{"id": "`+pcTrack+`", "name": "Found", "artists": [{"id": "a", "name": "A"}],
		 "album": {"name": "Al", "images": [{"url": "big"}, {"url": "mid"}, {"url": "small"}]}},
		{"id": "bad", "name": "Broken"}]}}`)
	got, err := p.SearchTracks(context.Background(), " love song ", 5)
	if err != nil {
		t.Fatal(err)
	}
	if q := (*seen)[0].query; q != "limit=5&q=love+song&type=track" {
		t.Errorf("query %q", q)
	}
	if len(got) != 1 || got[0].Name != "Found" || got[0].CoverImageURL != "mid" || got[0].URI != "spotify:track:"+pcTrack {
		t.Errorf("got %+v", got)
	}
}

func TestPlayerScope403AndRepeated401(t *testing.T) {
	p, _ := commandServer(t, http.StatusForbidden, `{"error":{"status":403,"message":"Insufficient client scope"}}`)
	if err := p.Pause(context.Background(), ""); !errors.Is(err, ErrInsufficientScope) {
		t.Errorf("scope 403: got %v", err)
	}

	p, _ = commandServer(t, http.StatusUnauthorized, `{"error":{"status":401,"message":"The access token expired"}}`)
	var se *StatusError
	if err := p.Pause(context.Background(), ""); !errors.As(err, &se) || se.Code != http.StatusUnauthorized {
		t.Errorf("repeated 401: got %v", err)
	}
}

func TestPlayerRetryResendsBody(t *testing.T) {
	var bodies []string
	p, tokens := playerServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := p.Transfer(context.Background(), pcDevice, true); err != nil {
		t.Fatal(err)
	}
	want := `{"device_ids":["dev-1"],"play":true}`
	if len(bodies) != 2 || !jsonEqual(bodies[0], want) || !jsonEqual(bodies[1], want) || tokens.forced.Load() != 1 {
		t.Errorf("bodies %q, forced %d", bodies, tokens.forced.Load())
	}
}

func TestArgErrorNamesField(t *testing.T) {
	p, _ := commandServer(t, http.StatusNoContent, "")
	var ae *ArgError
	if err := p.SetVolume(context.Background(), 101, ""); !errors.As(err, &ae) || ae.Field != "volume_percent" {
		t.Errorf("got %v", err)
	}
	if err := p.Play(context.Background(), PlayOptions{URIs: []string{pcTrack}, OffsetURI: "x"}); !errors.As(err, &ae) ||
		ae.Field != "offset_uri" {
		t.Errorf("got %v", err)
	}
}
