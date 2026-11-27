package listen

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const trackA = "4uLU6hMCjMI75M1A2tKUQC"

type memCache struct {
	mu   sync.Mutex
	m    map[string][]byte
	ttls map[string]time.Duration
}

func newMemCache() *memCache {
	return &memCache{m: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

func (c *memCache) get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if !ok {
		return nil, errors.New("miss")
	}
	return v, nil
}

func (c *memCache) set(_ context.Context, key string, raw []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key], c.ttls[key] = raw, ttl
}

// s3Stub answers HEAD requests by object key; unknown keys get def.
type s3Stub struct {
	mu     sync.Mutex
	status map[string]int
	ctype  map[string]string
	def    int
	paths  []string
	hits   atomic.Int32
	delay  time.Duration
}

func (s *s3Stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	time.Sleep(s.delay)
	s.mu.Lock()
	s.paths = append(s.paths, r.Method+" "+r.URL.Path)
	st, ok := s.status[r.URL.Path]
	ct := s.ctype[r.URL.Path]
	s.mu.Unlock()
	if !ok {
		st = s.def
	}
	if r.URL.Query().Get("X-Amz-Signature") == "" {
		st = http.StatusBadRequest
	}
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(st)
}

var audioNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func newTestLocator(t *testing.T, stub *s3Stub, exts ...string) (*Locator, *memCache, *bytes.Buffer) {
	t.Helper()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	cache := newMemCache()
	var logs bytes.Buffer
	if len(exts) == 0 {
		exts = []string{"mp3", "m4a", "flac"}
	}
	return &Locator{
		signer: &Signer{Endpoint: u, Region: "us-east-1", Bucket: "music", AccessKeyID: "id", Secret: "s", PathStyle: true},
		prefix: "audio/", exts: exts, ttl: 10 * time.Minute, cache: cache, client: srv.Client(),
		log: slog.New(slog.NewTextHandler(&logs, nil)), now: func() time.Time { return audioNow },
	}, cache, &logs
}

func TestLocateExtensionOrder(t *testing.T) {
	stub := &s3Stub{def: 404, status: map[string]int{"/music/audio/" + trackA + ".m4a": 200, "/music/audio/" + trackA + ".flac": 200}}
	l, cache, _ := newTestLocator(t, stub)
	got, err := l.Locate(context.Background(), trackA, 0)
	if err != nil || got == nil {
		t.Fatalf("locate = %+v, %v", got, err)
	}
	if !strings.Contains(got.URL, "/music/audio/"+trackA+".m4a?") || got.ContentType != "audio/mp4" {
		t.Errorf("got %+v", got)
	}
	if want := []string{"HEAD /music/audio/" + trackA + ".mp3", "HEAD /music/audio/" + trackA + ".m4a"}; strings.Join(stub.paths, ",") != strings.Join(want, ",") {
		t.Errorf("requests = %v", stub.paths)
	}
	if string(cache.m["spotify-tracker:listen:audio:"+trackA]) != `{"ext":"m4a","type":"audio/mp4"}` || cache.ttls["spotify-tracker:listen:audio:"+trackA] != time.Hour {
		t.Errorf("cache = %q %v", cache.m, cache.ttls)
	}
	// Cached: no more requests.
	if _, err := l.Locate(context.Background(), trackA, 0); err != nil || stub.hits.Load() != 2 {
		t.Errorf("cached lookup hit storage: %d %v", stub.hits.Load(), err)
	}
}

func TestLocateContentType(t *testing.T) {
	for _, c := range []struct{ ext, header, want string }{
		{"mp3", "audio/x-custom; charset=binary", "audio/x-custom"},
		{"mp3", "binary/octet-stream", "audio/mpeg"},
		{"opus", "", "audio/ogg"},
		{"flac", "text/plain", "audio/flac"},
	} {
		key := "/music/audio/" + trackA + "." + c.ext
		stub := &s3Stub{def: 404, status: map[string]int{key: 200}, ctype: map[string]string{key: c.header}}
		l, _, _ := newTestLocator(t, stub, c.ext)
		got, err := l.Locate(context.Background(), trackA, 0)
		if err != nil || got == nil || got.ContentType != c.want {
			t.Errorf("%s %q: %+v %v, want %s", c.ext, c.header, got, err, c.want)
		}
	}
}

func TestLocateMissing(t *testing.T) {
	stub := &s3Stub{def: 404}
	l, cache, logs := newTestLocator(t, stub)
	got, err := l.Locate(context.Background(), trackA, 0)
	if err != nil || got != nil {
		t.Fatalf("locate = %+v, %v", got, err)
	}
	if string(cache.m["spotify-tracker:listen:audio:"+trackA]) != `{}` || cache.ttls["spotify-tracker:listen:audio:"+trackA] != 5*time.Minute {
		t.Errorf("missing not cached: %q", cache.m)
	}
	if strings.Contains(logs.String(), "WARN") {
		t.Errorf("404s logged a warning: %s", logs)
	}
	if got, _ := l.Locate(context.Background(), trackA, 0); got != nil || stub.hits.Load() != 3 {
		t.Errorf("cached missing hit storage again: %d", stub.hits.Load())
	}
}

func TestLocateAllForbiddenWarnsOnce(t *testing.T) {
	stub := &s3Stub{def: 403}
	l, _, logs := newTestLocator(t, stub)
	if got, err := l.Locate(context.Background(), trackA, 0); err != nil || got != nil {
		t.Fatalf("locate = %+v, %v", got, err)
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 || !strings.Contains(logs.String(), "ListBucket") {
		t.Errorf("warnings = %d: %s", n, logs)
	}
	if strings.Contains(logs.String(), "X-Amz") {
		t.Error("presigned URL in logs")
	}
}

func TestLocateServerErrorNotCached(t *testing.T) {
	stub := &s3Stub{def: 500}
	l, cache, _ := newTestLocator(t, stub)
	got, err := l.Locate(context.Background(), trackA, 0)
	if err == nil || got != nil {
		t.Fatalf("locate = %+v, %v", got, err)
	}
	if strings.Contains(err.Error(), "X-Amz") || strings.Contains(err.Error(), "http") {
		t.Errorf("error leaks the URL: %v", err)
	}
	if len(cache.m) != 0 {
		t.Errorf("error cached: %q", cache.m)
	}
}

func TestLocateNetworkErrorHidesURL(t *testing.T) {
	stub := &s3Stub{def: 200}
	l, _, _ := newTestLocator(t, stub)
	l.signer.Endpoint = &url.URL{Scheme: "http", Host: "127.0.0.1:1"}
	_, err := l.Locate(context.Background(), trackA, 0)
	if err == nil || strings.Contains(err.Error(), "X-Amz") || strings.Contains(err.Error(), "127.0.0.1:1/") {
		t.Errorf("err = %v", err)
	}
}

func TestLocateInvalidID(t *testing.T) {
	stub := &s3Stub{def: 200}
	l, _, _ := newTestLocator(t, stub)
	for _, id := range []string{"", "short", "../../etc/passwd-xxxxxx", trackA + "x", "4uLU6hMCjMI75M1A2tKU-C"} {
		if got, err := l.Locate(context.Background(), id, 0); got != nil || err != nil {
			t.Errorf("%q: %+v %v", id, got, err)
		}
	}
	if stub.hits.Load() != 0 {
		t.Error("invalid id reached storage")
	}
}

func TestLocateSingleflight(t *testing.T) {
	stub := &s3Stub{def: 200, delay: 100 * time.Millisecond}
	l, _, _ := newTestLocator(t, stub)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := l.Locate(context.Background(), trackA, 0); err != nil || got == nil {
				t.Errorf("locate = %+v, %v", got, err)
			}
		})
	}
	wg.Wait()
	if n := stub.hits.Load(); n != 1 {
		t.Errorf("storage hit %d times, want 1", n)
	}
}

func TestLocateTTL(t *testing.T) {
	stub := &s3Stub{def: 200}
	l, _, _ := newTestLocator(t, stub)
	for _, c := range []struct {
		remaining time.Duration
		want      time.Duration
		expires   string
	}{
		{0, 10 * time.Minute, "X-Amz-Expires=600&"},
		{3 * time.Minute, 10 * time.Minute, "X-Amz-Expires=600&"},
		{20 * time.Minute, 21 * time.Minute, "X-Amz-Expires=1260&"},
		{3 * time.Hour, time.Hour, "X-Amz-Expires=3600&"},
	} {
		got, err := l.Locate(context.Background(), trackA, c.remaining)
		if err != nil || got == nil {
			t.Fatal(err)
		}
		if !got.ExpiresAt.Equal(audioNow.Add(c.want)) || !strings.Contains(got.URL, c.expires) {
			t.Errorf("remaining %v: expires %v %s", c.remaining, got.ExpiresAt, got.URL)
		}
	}
}
