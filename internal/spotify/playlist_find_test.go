package spotify

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestFindOwnedPlaylist(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			_, _ = fmt.Fprint(w, `{"id":"bao","display_name":"Bao"}`)
		case "/v1/me/playlists":
			_, _ = fmt.Fprint(w, `{"items":[
				{"id":"other","name":"Top 50 · 2026","owner":{"id":"someone"}},
				{"id":"near","name":"Top 50 · 2026 ","owner":{"id":"bao"}},
				{"id":"mine","name":"Top 50 · 2026","owner":{"id":"bao"},"external_urls":{"spotify":"https://open.spotify.com/playlist/mine"}}
			],"next":null}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	})
	id, link, err := m.FindOwnedPlaylist(context.Background(), "Top 50 · 2026")
	if err != nil || id != "mine" || link != "https://open.spotify.com/playlist/mine" {
		t.Fatalf("got %q %q %v", id, link, err)
	}
	id, link, err = m.FindOwnedPlaylist(context.Background(), "Top 50 · 2025")
	if err != nil || id != "" || link != "" {
		t.Fatalf("missing name: got %q %q %v", id, link, err)
	}
}

func TestFindOwnedPlaylistErrors(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/me" {
				_, _ = fmt.Fprint(w, `{"id":"bao"}`)
				return
			}
			w.WriteHeader(status)
		})
		if id, _, err := m.FindOwnedPlaylist(context.Background(), "x"); err == nil || id != "" {
			t.Fatalf("%d: want error, got %q", status, id)
		}
	}
}
