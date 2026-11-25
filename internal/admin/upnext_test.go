package admin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

func TestReadUpNext(t *testing.T) {
	up := domain.UpNext{ForTrackID: "cur", Items: []domain.UpNextItem{{Position: 0, TrackID: "n", Name: "Next"}}}
	body, _ := json.Marshal(up)
	got := readUpNext(context.Background(), func(context.Context) ([]byte, error) { return body, nil })
	if got == nil || got.ForTrackID != "cur" || len(got.Items) != 1 || got.Items[0].TrackID != "n" {
		t.Fatalf("got %+v", got)
	}
	for name, get := range map[string]func(context.Context) ([]byte, error){
		"redis error": func(context.Context) ([]byte, error) { return nil, errors.New("down") },
		"missing":     func(context.Context) ([]byte, error) { return nil, nil },
		"bad json":    func(context.Context) ([]byte, error) { return []byte("{"), nil },
		"empty list":  func(context.Context) ([]byte, error) { return []byte(`{"items":[]}`), nil },
	} {
		if got := readUpNext(context.Background(), get); got != nil {
			t.Errorf("%s: got %+v, want nil", name, got)
		}
	}
	if (&API{}).upNext(context.Background()) != nil {
		t.Error("no redis must give nil")
	}
}
