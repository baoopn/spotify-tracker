package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestGetJSONStatusError(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{{http.StatusBadRequest, true}, {http.StatusInternalServerError, false}} {
		m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		_, err := m.ArtistProfile(context.Background(), "x")
		var se *StatusError
		if !errors.As(err, &se) || se.Code != tc.status || se.Permanent() != tc.permanent {
			t.Fatalf("%d: got %v", tc.status, err)
		}
	}
}

func TestArtistProfileGenres(t *testing.T) {
	long := strings.Repeat("x", maxGenreLen+1)
	m, _ := writeManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/artists/a1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"a1","images":[{"url":"https://i.scdn.co/image/640","width":640},
			{"url":"https://i.scdn.co/image/320","width":320}],
			"genres":[" indie pop ","","bedroom pop","indie pop","` + long + `"]}`))
	})
	p, err := m.ArtistProfile(context.Background(), "a1")
	if err != nil || !p.Found || p.ImageURL != "https://i.scdn.co/image/320" {
		t.Fatalf("got %+v, %v", p, err)
	}
	if want := []string{"indie pop", "bedroom pop"}; !reflect.DeepEqual(p.Genres, want) {
		t.Errorf("genres = %q, want %q", p.Genres, want)
	}
}

func TestArtistProfileGenresCapped(t *testing.T) {
	genres := make([]string, maxArtistGenres+5)
	for i := range genres {
		genres[i] = fmt.Sprintf("%q", fmt.Sprintf("g%d", i))
	}
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"a1","genres":[` + strings.Join(genres, ",") + `]}`))
	})
	p, err := m.ArtistProfile(context.Background(), "a1")
	if err != nil || len(p.Genres) != maxArtistGenres || p.Genres[0] != "g0" {
		t.Fatalf("got %q, %v", p.Genres, err)
	}
}

func TestArtistProfileMissingGenres(t *testing.T) {
	m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"a1"}`))
	})
	p, err := m.ArtistProfile(context.Background(), "a1")
	if err != nil || !p.Found || p.ImageURL != "" || p.Genres == nil || len(p.Genres) != 0 {
		t.Fatalf("got %+v (genres nil: %v), %v", p, p.Genres == nil, err)
	}
}

func TestArtistProfileNotFound(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		p, err := m.ArtistProfile(context.Background(), "a1")
		if err != nil || p.Found || p.Genres != nil {
			t.Fatalf("%d: got %+v, %v", status, p, err)
		}
	}
}
