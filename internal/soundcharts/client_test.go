package soundcharts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, code int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-app-id") != "app" || r.Header.Get("x-api-key") != "key" {
			t.Errorf("missing credentials headers")
		}
		if r.URL.Path != "/api/v2.25/song/by-isrc/USRC17607839" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "app", "key", 100, 1, nil)
}

func TestSongByISRC(t *testing.T) {
	c := newTestClient(t, 200, `{"type":"song","object":{
		"audio":{"acousticness":0.574,"danceability":0.546,"energy":0.312,"instrumentalness":1.85e-6,
			"key":8,"liveness":0.369,"loudness":-11.633,"mode":1,"speechiness":0.0502,"tempo":177.997,
			"timeSignature":4,"valence":0.575},
		"genres":[{"root":"alternative","sub":["alternative","indie"]},{"root":"","sub":[]}]}}`)
	song, err := c.SongByISRC(context.Background(), "USRC17607839")
	if err != nil {
		t.Fatal(err)
	}
	if !song.HasAudio || song.Features.Key == nil || *song.Features.Key != 8 || *song.Features.Tempo != 177.997 {
		t.Errorf("features mapped wrong: %+v", song.Features)
	}
	if len(song.Genres) != 1 || song.Genres[0].Root != "alternative" || len(song.Genres[0].Sub) != 2 {
		t.Errorf("genres mapped wrong: %+v", song.Genres)
	}
}

func TestSongByISRCNotFound(t *testing.T) {
	c := newTestClient(t, 404, `{"errors":[]}`)
	song, err := c.SongByISRC(context.Background(), "USRC17607839")
	if err != nil || song != nil {
		t.Fatalf("got %v, %v; want nil, nil", song, err)
	}
}

func TestSongByISRCNoAudio(t *testing.T) {
	c := newTestClient(t, 200, `{"type":"song","object":{"genres":[{"root":"pop","sub":[]}]}}`)
	song, err := c.SongByISRC(context.Background(), "USRC17607839")
	if err != nil {
		t.Fatal(err)
	}
	if song.HasAudio || len(song.Genres) != 1 {
		t.Errorf("got %+v", song)
	}
}

func TestSongByISRCRetryable(t *testing.T) {
	c := newTestClient(t, 503, `unavailable`)
	_, err := c.SongByISRC(context.Background(), "USRC17607839")
	var re *RetryableError
	if !errors.As(err, &re) || re.Status != 503 {
		t.Fatalf("want RetryableError 503, got %v", err)
	}
}
