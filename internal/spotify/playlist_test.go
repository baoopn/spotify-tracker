package spotify

import (
	"encoding/json"
	"testing"
)

func TestPlaylistFromResponse(t *testing.T) {
	for name, body := range map[string]string{
		"2026 items": `{"id":"p1","name":"Chill","public":true,"owner":{"id":"owner1","display_name":"Owner"},
			"images":[{"url":"https://mosaic.scdn.co/640/x"}],"external_urls":{"spotify":"https://open.spotify.com/playlist/p1"},
			"uri":"spotify:playlist:p1","items":{"total":42}}`,
		"legacy tracks": `{"id":"p1","name":"Chill","public":true,"owner":{"id":"owner1","display_name":"Owner"},
			"images":[{"url":"https://mosaic.scdn.co/640/x"}],"uri":"spotify:playlist:p1","tracks":{"total":42}}`,
	} {
		var pr playlistResponse
		if err := json.Unmarshal([]byte(body), &pr); err != nil {
			t.Fatal(err)
		}
		p := playlistFromResponse(pr)
		if !p.Available || p.Name != "Chill" || p.OwnerName != "Owner" || p.TracksTotal == nil || *p.TracksTotal != 42 ||
			p.Public == nil || !*p.Public || p.CoverImageURL == "" {
			t.Errorf("%s: got %+v", name, p)
		}
	}
}

func TestPickImage(t *testing.T) {
	var ar artistResponse
	_ = json.Unmarshal([]byte(`{"images":[
		{"url":"https://i.scdn.co/image/640","width":640,"height":640},
		{"url":"https://i.scdn.co/image/320","width":320,"height":320},
		{"url":"https://i.scdn.co/image/160","width":160,"height":160}]}`), &ar)
	if got := pickImage(ar, 300); got != "https://i.scdn.co/image/320" {
		t.Errorf("pickImage = %q, want the 320px image", got)
	}
	if got := pickImage(ar, 1000); got != "https://i.scdn.co/image/640" {
		t.Errorf("pickImage (none big enough) = %q, want the largest", got)
	}
	if got := pickImage(artistResponse{}, 300); got != "" {
		t.Errorf("pickImage(no images) = %q", got)
	}
}
