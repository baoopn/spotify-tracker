package httpapi

import (
	"net/url"
	"testing"
)

func TestCacheKeyOnlyReadParams(t *testing.T) {
	params := []string{"range", "limit"}
	key := func(raw string) string {
		q, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		return cacheKey("g1", "/api/v1/stats/top-tracks", q, params)
	}
	base := key("range=30d&limit=10")
	for _, same := range []string{
		"limit=10&range=30d",                 // order
		"range=30d&limit=10&cb=123&utm=x",    // unknown params
		"range=30d&limit=10&limit=20",        // duplicates: the first value wins, like Query().Get
		"range=30d&limit=10&limit=&range=1y", // later values are ignored
	} {
		if got := key(same); got != base {
			t.Errorf("%q: key differs from the base", same)
		}
	}
	if key("") != key("range=&limit=") || key("") != key("junk=1") {
		t.Error("empty and unknown params must share the default key")
	}
	for _, diff := range []string{"range=1y&limit=10", "range=30d&limit=11", "range=30d"} {
		if key(diff) == base {
			t.Errorf("%q: same key as the base", diff)
		}
	}
	q, _ := url.ParseQuery("range=30d&limit=10")
	if cacheKey("g2", "/api/v1/stats/top-tracks", q, params) == base ||
		cacheKey("g1", "/api/v1/stats/top-artists", q, params) == base {
		t.Error("generation and path must be part of the key")
	}
}
