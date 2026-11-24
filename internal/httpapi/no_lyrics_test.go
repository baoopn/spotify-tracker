package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// Lyrics are admin and MCP only. The one public exception, the now-playing
// window, is registered outside v1Routes, so MCP (V1Handler) cannot reach it.
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

// ...and no public (or shared admin track) JSON type may carry lyrics; the
// window has its own type (nowLyricsBody).
func TestNoLyricsInTrackJSON(t *testing.T) {
	for _, v := range []any{domain.TrackDetail{}, domain.TrackView{}, domain.NowPlaying{}, store.TrackAdminDetail{}} {
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

// The full router has exactly one lyrics route: the now-playing window.
func TestOnlyOneLyricsRoute(t *testing.T) {
	r, ok := NewRouter(nil, nil, slog.New(slog.DiscardHandler), Options{}).(chi.Routes)
	if !ok {
		t.Fatal("router is not chi.Routes")
	}
	var got []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.Contains(strings.ToLower(route), "lyric") {
			got = append(got, method+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "GET /api/v1/now-playing/lyrics" {
		t.Errorf("lyrics routes = %v", got)
	}
}

// MCP dispatches through V1Handler, which must not serve the window.
func TestV1HandlerHasNoLyrics(t *testing.T) {
	h := &Handler{log: slog.New(slog.DiscardHandler)}
	rec := httptest.NewRecorder()
	h.V1Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/now-playing/lyrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("V1Handler served the lyrics window: %d %s", rec.Code, rec.Body.String())
	}
}
