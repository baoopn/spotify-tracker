package spotify

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestGetJSONStatusError(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{{http.StatusBadRequest, true}, {http.StatusInternalServerError, false}} {
		m, _ := writeManager(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		_, err := m.ArtistImage(context.Background(), "x")
		var se *StatusError
		if !errors.As(err, &se) || se.Code != tc.status || se.Permanent() != tc.permanent {
			t.Fatalf("%d: got %v", tc.status, err)
		}
	}
}
