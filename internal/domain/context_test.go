package domain

import "testing"

func TestPlaybackContextID(t *testing.T) {
	cases := map[string]string{
		"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M": "37i9dQZF1DXcBWIGoYBM5M",
		"spotify:album:1kOV94kOm4oCBzloDcW9ry":    "1kOV94kOm4oCBzloDcW9ry",
		"spotify:user:owner1:collection":          "collection",
		"https://open.spotify.com/x":              "",
		"":                                        "",
	}
	for uri, want := range cases {
		if got := (PlaybackContext{URI: uri}).ID(); got != want {
			t.Errorf("ID(%q) = %q, want %q", uri, got, want)
		}
	}
}
