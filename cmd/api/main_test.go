package main

import (
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/staticsite"
)

// The public page may frame only the Spotify embed player.
func TestPublicSiteCSPAllowsSpotifyEmbed(t *testing.T) {
	csp := staticsite.BuildCSP(nil, publicSiteOptions().ExtraCSP)
	if !strings.Contains(csp, "frame-src https://open.spotify.com;") {
		t.Errorf("frame-src should allow only open.spotify.com: %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("frame-ancestors must stay 'none': %s", csp)
	}
}
