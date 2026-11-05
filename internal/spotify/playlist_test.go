package spotify

import (
	"encoding/json"
	"testing"
)

func TestPlaylistFromResponse(t *testing.T) {
	for name, body := range map[string]string{
		"2026 items": `{"id":"p1","name":"Chill","public":true,"owner":{"id":"bao","display_name":"Bao"},
			"images":[{"url":"https://mosaic.scdn.co/640/x"}],"external_urls":{"spotify":"https://open.spotify.com/playlist/p1"},
			"uri":"spotify:playlist:p1","items":{"total":42}}`,
		"legacy tracks": `{"id":"p1","name":"Chill","public":true,"owner":{"id":"bao","display_name":"Bao"},
			"images":[{"url":"https://mosaic.scdn.co/640/x"}],"uri":"spotify:playlist:p1","tracks":{"total":42}}`,
	} {
		var pr playlistResponse
		if err := json.Unmarshal([]byte(body), &pr); err != nil {
			t.Fatal(err)
		}
		p := playlistFromResponse(pr)
		if !p.Available || p.Name != "Chill" || p.OwnerName != "Bao" || p.TracksTotal == nil || *p.TracksTotal != 42 ||
			p.Public == nil || !*p.Public || p.CoverImageURL == "" {
			t.Errorf("%s: got %+v", name, p)
		}
	}
}
