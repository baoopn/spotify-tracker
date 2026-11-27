package listen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/store"
)

func TestPresignBucket(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	path := &Signer{Endpoint: &url.URL{Scheme: "https", Host: "s3.example.com"}, Region: "us-east-1", Bucket: "music",
		AccessKeyID: "id", Secret: "secret", PathStyle: true}
	got := path.PresignBucket(http.MethodGet, map[string]string{"list-type": "2", "prefix": "audio/x y",
		"max-keys": "1000", "continuation-token": "a+b/c="}, time.Minute, now)
	u, err := url.Parse(got)
	if err != nil || u.Path != "/music" || u.Host != "s3.example.com" {
		t.Fatalf("url = %s", got)
	}
	// Sorted, SigV4-encoded query (space as %20, / and = encoded).
	want := "X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=id%2F20261126%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20261126T120000Z&X-Amz-Expires=60&X-Amz-SignedHeaders=host&continuation-token=a%2Bb%2Fc%3D" +
		"&list-type=2&max-keys=1000&prefix=audio%2Fx%20y&X-Amz-Signature="
	if !strings.HasPrefix(u.RawQuery, want) || len(u.RawQuery) != len(want)+64 {
		t.Errorf("query =\n%s\nwant prefix\n%s", u.RawQuery, want)
	}
	// Every param is signed.
	other := path.PresignBucket(http.MethodGet, map[string]string{"list-type": "2", "prefix": "audio/"}, time.Minute, now)
	if other[len(other)-64:] == got[len(got)-64:] {
		t.Error("params not signed")
	}
	vhost := *path
	vhost.PathStyle = false
	if got := vhost.PresignBucket(http.MethodGet, nil, time.Minute, now); !strings.HasPrefix(got, "https://music.s3.example.com/?X-Amz-") {
		t.Errorf("virtual host = %s", got)
	}
}

const id1, id2, id3 = "4uLU6hMCjMI75M1A2tKUQC", "0VjIjW4GlUZAMYd2vXMi3b", "7qiZfU4dY1lWllzX7mPBI3"

func TestPickFiles(t *testing.T) {
	objs := []listObject{
		{Key: "audio/" + id1 + ".flac", Size: 30, LastModified: "2026-11-01T10:00:00.000Z", ETag: `"abc"`},
		{Key: "audio/" + id1 + ".mp3", Size: 10},
		{Key: "audio/" + id2 + ".m4a", Size: 20},
		{Key: "audio/" + id3 + ".wav", Size: 5},      // extension not allowed
		{Key: "audio/" + id3 + ".MP3", Size: 5},      // upper case
		{Key: "audio/sub/" + id3 + ".mp3", Size: 5},  // nested
		{Key: "audio/short.mp3", Size: 5},            // not a track ID
		{Key: "other/" + id3 + ".mp3", Size: 5},      // outside the prefix
		{Key: "audio/" + id3 + ".mp3.part", Size: 5}, // partial upload
		{Key: "audio/" + id3 + "x.mp3", Size: 5},     // 23 characters
		{Key: "audio/" + id1 + ".m4a", Size: 15},     // lower rank than mp3
		{Key: "audio/" + id2 + ".mp3", Size: 25},     // better rank than m4a
		{Key: "audio/" + id2 + ".flac", Size: 40},    // worse rank
	}
	got := pickFiles(objs, "audio/", []string{"mp3", "m4a", "flac"})
	if len(got) != 2 {
		t.Fatalf("files = %+v", got)
	}
	if got[0].TrackID != id1 || got[0].Ext != "mp3" || got[0].SizeBytes != 10 || *got[0].ContentType != "audio/mpeg" {
		t.Errorf("file 1 = %+v", got[0])
	}
	if got[1].TrackID != id2 || got[1].Ext != "mp3" || got[1].SizeBytes != 25 {
		t.Errorf("file 2 = %+v", got[1])
	}
	one := pickFiles(objs[:1], "audio/", []string{"flac"})
	if len(one) != 1 || *one[0].ETag != "abc" || one[0].LastModified == nil ||
		!one[0].LastModified.Equal(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("metadata = %+v", one)
	}
}

func TestParseListPage(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>music</Name><Prefix>audio/</Prefix><KeyCount>2</KeyCount><MaxKeys>1000</MaxKeys>
  <IsTruncated>true</IsTruncated><NextContinuationToken>tok==</NextContinuationToken>
  <Contents><Key>audio/` + id1 + `.mp3</Key><LastModified>2026-11-01T10:00:00.000Z</LastModified><ETag>"e1"</ETag><Size>123</Size></Contents>
  <Contents><Key>audio/readme.txt</Key><Size>4</Size></Contents>
</ListBucketResult>`
	res, err := parseListPage(strings.NewReader(xml))
	if err != nil || !res.IsTruncated || res.NextContinuationToken != "tok==" || len(res.Contents) != 2 || res.Contents[0].Size != 123 {
		t.Fatalf("parsed = %+v %v", res, err)
	}
	if _, err := parseListPage(strings.NewReader("<nope")); err == nil {
		t.Error("bad XML parsed")
	}
}

// bucketStub serves ListObjectsV2 pages; fail makes page N answer 500.
type bucketStub struct {
	pages [][]string // keys per page
	fail  int        // 1-based page that fails (0 = none)
	calls int
	mu    sync.Mutex
}

func (b *bucketStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	q := r.URL.Query()
	if q.Get("list-type") != "2" || q.Get("X-Amz-Signature") == "" || q.Get("prefix") != "audio/" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	page := 0
	if tok := q.Get("continuation-token"); tok != "" {
		_, _ = fmt.Sscanf(tok, "p%d", &page)
	}
	if b.fail == page+1 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	buf.WriteString(`<ListBucketResult>`)
	if page+1 < len(b.pages) {
		fmt.Fprintf(&buf, `<IsTruncated>true</IsTruncated><NextContinuationToken>p%d</NextContinuationToken>`, page+1)
	}
	for _, k := range b.pages[page] {
		fmt.Fprintf(&buf, `<Contents><Key>%s</Key><Size>1</Size></Contents>`, k)
	}
	buf.WriteString(`</ListBucketResult>`)
	_, _ = w.Write(buf.Bytes())
}

type fakeSyncStore struct {
	rows  map[string]store.TrackAudio
	calls int
}

func (f *fakeSyncStore) ReplaceTrackAudio(_ context.Context, files []store.TrackAudio) ([]string, []string, error) {
	f.calls++
	var added, removed []string
	next := map[string]store.TrackAudio{}
	for _, a := range files {
		if _, ok := f.rows[a.TrackID]; !ok {
			added = append(added, a.TrackID)
		}
		next[a.TrackID] = a
	}
	for id := range f.rows {
		if _, ok := next[id]; !ok {
			removed = append(removed, id)
		}
	}
	f.rows = next
	return added, removed, nil
}

type memSyncKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (k *memSyncKV) setNX(_ context.Context, key string, _ time.Duration) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[key]; ok {
		return false, nil
	}
	k.m[key] = []byte("1")
	return true, nil
}
func (k *memSyncKV) get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v, ok := k.m[key]; ok {
		return v, nil
	}
	return nil, errors.New("nil")
}
func (k *memSyncKV) set(_ context.Context, key string, raw []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = raw
	return nil
}
func (k *memSyncKV) del(_ context.Context, keys ...string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, key := range keys {
		delete(k.m, key)
	}
	return nil
}

func newTestSyncer(t *testing.T, b *bucketStub) (*Syncer, *fakeSyncStore, *memSyncKV) {
	t.Helper()
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	db := &fakeSyncStore{rows: map[string]store.TrackAudio{"gone": {TrackID: "gone"}}}
	kv := &memSyncKV{m: map[string][]byte{}}
	return &Syncer{signer: &Signer{Endpoint: u, Region: "us-east-1", Bucket: "music", AccessKeyID: "id", Secret: "s", PathStyle: true},
		prefix: "audio/", exts: []string{"mp3", "m4a"}, client: srv.Client(), db: db, kv: kv,
		log: slog.New(slog.DiscardHandler), now: time.Now}, db, kv
}

func TestSyncPagesAndReplaces(t *testing.T) {
	b := &bucketStub{pages: [][]string{{"audio/" + id1 + ".mp3", "audio/junk.txt"}, {"audio/" + id2 + ".m4a"}}}
	s, db, kv := newTestSyncer(t, b)
	kv.m["spotify-tracker:listen:audio:"+id1] = []byte(`{}`)
	kv.m["spotify-tracker:listen:audio:gone"] = []byte(`{"ext":"mp3"}`)
	res, err := s.SyncNow(context.Background())
	if err != nil || res.Files != 2 || res.Added != 2 || res.Removed != 1 || b.calls != 2 {
		t.Fatalf("sync = %+v %v (calls %d)", res, err, b.calls)
	}
	if _, ok := db.rows[id2]; !ok || len(db.rows) != 2 {
		t.Errorf("rows = %v", db.rows)
	}
	if _, ok := kv.m["spotify-tracker:listen:audio:"+id1]; ok {
		t.Error("missing-cache entry kept for a found file")
	}
	if _, ok := kv.m["spotify-tracker:listen:audio:gone"]; ok {
		t.Error("found-cache entry kept for a removed file")
	}
	if _, ok := kv.m["spotify-tracker:listen:audio-sync:lock"]; ok {
		t.Error("lock not released")
	}
	st, ok := s.Status(context.Background())
	if !ok || st.Files != 2 || st.LastError != "" || st.LastSyncAt == nil {
		t.Errorf("status = %+v %v", st, ok)
	}
}

func TestSyncPartialListingChangesNothing(t *testing.T) {
	b := &bucketStub{pages: [][]string{{"audio/" + id1 + ".mp3"}, {"audio/" + id2 + ".mp3"}}, fail: 2}
	s, db, _ := newTestSyncer(t, b)
	_, err := s.SyncNow(context.Background())
	if err == nil || strings.Contains(err.Error(), "X-Amz") || strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
	if db.calls != 0 || len(db.rows) != 1 {
		t.Errorf("partial listing changed rows: calls %d rows %v", db.calls, db.rows)
	}
	if st, _ := s.Status(context.Background()); !strings.Contains(st.LastError, "500") || st.LastSyncAt == nil {
		t.Errorf("status = %+v", st)
	}
}

func TestSyncNowLockAndCooldown(t *testing.T) {
	b := &bucketStub{pages: [][]string{{}}}
	s, _, kv := newTestSyncer(t, b)
	kv.m["spotify-tracker:listen:audio-sync:lock"] = []byte("1")
	if _, err := s.SyncNow(context.Background()); !errors.Is(err, ErrSyncRunning) {
		t.Errorf("locked err = %v", err)
	}
	// Still running inside the cooldown: running wins.
	if _, err := s.SyncNow(context.Background()); !errors.Is(err, ErrSyncRunning) {
		t.Errorf("locked in cooldown err = %v", err)
	}
	delete(kv.m, "spotify-tracker:listen:audio-sync:lock")
	if _, err := s.SyncNow(context.Background()); !errors.Is(err, ErrSyncCooldown) {
		t.Errorf("cooldown err = %v", err)
	}
	delete(kv.m, "spotify-tracker:listen:audio-sync:cooldown")
	if _, err := s.SyncNow(context.Background()); err != nil {
		t.Errorf("after cooldown err = %v", err)
	}
}

func TestSyncRunNilOrOff(t *testing.T) {
	var s *Syncer
	if err := s.Run(context.Background()); err != nil {
		t.Error(err)
	}
	if err := (&Syncer{}).Run(context.Background()); err != nil {
		t.Error(err)
	}
}
