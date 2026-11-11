package spotify

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/settings"
)

// A refresh that rotates the token but lacks the required scope must still
// keep the rotated token: the old one may already be revoked.
func TestAccessTokenMissingScopeKeepsRotatedToken(t *testing.T) {
	fastSaveBackoff(t)
	acc := &accountsServer{reply: func(rt string) (int, map[string]any) {
		b := tokenBody("")
		if rt == "r1" {
			b = tokenBody("r2")
		}
		b["scope"] = "user-read-private"
		return http.StatusOK, b
	}}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}}
	m := newTestManager(t, acc, st, nil)

	if _, _, err := m.AccessToken(context.Background(), true); !errors.Is(err, ErrMissingScope) {
		t.Fatalf("err = %v, want ErrMissingScope", err)
	}
	if got := st.stored(); got != "r2" && m.unsaved != "r2" {
		t.Errorf("rotated token dropped: stored = %q, unsaved = %q, want r2 in either", got, m.unsaved)
	}
	_, _, _ = m.AccessToken(context.Background(), true)
	if got := acc.refreshes(); !slices.Equal(got, []string{"r1", "r2"}) {
		t.Errorf("refresh tokens sent = %v, want [r1 r2]", got)
	}
}
