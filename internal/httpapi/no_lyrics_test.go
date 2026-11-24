package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// Lyrics are admin and MCP only: the public API must have no lyrics route.
func TestNoPublicLyricsRoute(t *testing.T) {
	r := chi.NewRouter()
	(&Handler{}).v1Routes(r)
	n := 0
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		if strings.Contains(strings.ToLower(route), "lyric") {
			t.Errorf("public route %s %s serves lyrics", method, route)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("walk: %v (%d routes)", err, n)
	}
}

// ...and no public (or shared admin track) JSON type may carry lyrics.
func TestNoLyricsInTrackJSON(t *testing.T) {
	for _, v := range []any{domain.TrackDetail{}, domain.TrackView{}, store.TrackAdminDetail{}} {
		checkNoLyricField(t, reflect.TypeOf(v), map[reflect.Type]bool{})
	}
}

func checkNoLyricField(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := range typ.NumField() {
		f := typ.Field(i)
		if strings.Contains(strings.ToLower(f.Tag.Get("json")+f.Name), "lyric") {
			t.Errorf("%s.%s carries lyrics", typ.Name(), f.Name)
		}
		checkNoLyricField(t, f.Type, seen)
	}
}
