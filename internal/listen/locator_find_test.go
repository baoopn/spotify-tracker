package listen

import (
	"context"
	"strings"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/store"
)

// Find answers from the cache or HEAD requests and never signs a URL.
func TestLocatorFind(t *testing.T) {
	key := "/music/audio/" + trackA + ".m4a"
	stub := &s3Stub{def: 404, status: map[string]int{key: 200}}
	l, cache, _ := newTestLocator(t, stub)
	db := &fakeAudioDB{rows: map[string]store.TrackAudio{}}
	l.db = db
	ext, found, err := l.Find(context.Background(), trackA)
	if err != nil || !found || ext != "m4a" {
		t.Fatalf("find = %q %v %v", ext, found, err)
	}
	if len(db.ups) != 1 {
		t.Errorf("found file not recorded: %+v", db.ups)
	}
	hits := stub.hits.Load()
	if _, found, _ := l.Find(context.Background(), trackA); !found || stub.hits.Load() != hits {
		t.Error("cached find hit storage again")
	}
	for _, v := range cache.m {
		if strings.Contains(string(v), "X-Amz") {
			t.Errorf("signed URL cached: %s", v)
		}
	}
	if _, found, err := l.Find(context.Background(), "0VjIjW4GlUZAMYd2vXMi3b"); found || err != nil {
		t.Errorf("missing = %v %v", found, err)
	}
	if _, found, _ := l.Find(context.Background(), "bad"); found {
		t.Error("invalid id found")
	}
}
