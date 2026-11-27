package listen

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

const (
	syncLockTTL     = 10 * time.Minute // longer than any sync
	syncRunTimeout  = 9 * time.Minute  // a scheduled sync
	syncNowTimeout  = 60 * time.Second // a sync started from the admin
	syncCooldown    = 30 * time.Second // between syncs started from the admin
	syncFirstDelay  = 30 * time.Second
	syncMaxPages    = 1000 // 1M keys at 1000 per page
	listPageTimeout = 20 * time.Second
	cacheDelBatch   = 500
)

var (
	// ErrSyncRunning means another sync (on any replica) holds the lock.
	ErrSyncRunning = errors.New("an audio sync is already running")
	// ErrSyncCooldown means a sync was started less than 30 s ago.
	ErrSyncCooldown = errors.New("an audio sync ran less than 30 seconds ago")
)

// SyncResult is the outcome of one bucket sync.
type SyncResult struct {
	Files   int   `json:"files"`
	Added   int   `json:"added"`
	Removed int   `json:"removed"`
	TookMS  int64 `json:"took_ms"`
}

// SyncStatus is the last sync, kept in Redis. LastSyncAt is the time of the
// last attempt; Files is from the last complete sync.
type SyncStatus struct {
	LastSyncAt *time.Time `json:"last_sync_at"`
	Files      int        `json:"files"`
	LastError  string     `json:"last_error"`
}

// syncStore replaces the track_audio rows (implemented by *store.Store).
type syncStore interface {
	ReplaceTrackAudio(ctx context.Context, files []store.TrackAudio) (added, removed []string, err error)
}

// syncKV is the Redis part of the sync (tests replace it).
type syncKV interface {
	setNX(ctx context.Context, key string, ttl time.Duration) (bool, error)
	get(ctx context.Context, key string) ([]byte, error)
	set(ctx context.Context, key string, raw []byte) error
	del(ctx context.Context, keys ...string) error
}

type redisSyncKV struct{ rdb *redis.Client }

func (r redisSyncKV) setNX(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return r.rdb.SetNX(ctx, key, time.Now().UTC().Format(time.RFC3339), ttl).Result()
}
func (r redisSyncKV) get(ctx context.Context, key string) ([]byte, error) {
	return r.rdb.Get(ctx, key).Bytes()
}
func (r redisSyncKV) set(ctx context.Context, key string, raw []byte) error {
	return r.rdb.Set(ctx, key, raw, 0).Err()
}
func (r redisSyncKV) del(ctx context.Context, keys ...string) error {
	return r.rdb.Del(ctx, keys...).Err()
}

// Syncer lists the audio bucket and records the files in track_audio.
type Syncer struct {
	signer   *Signer
	prefix   string
	exts     []string
	interval time.Duration
	client   *http.Client
	db       syncStore
	kv       syncKV
	log      *slog.Logger
	now      func() time.Time
}

// NewSyncer builds a Syncer, or nil when S3 is not configured.
func NewSyncer(cfg config.Listen, db syncStore, rdb *redis.Client, log *slog.Logger) *Syncer {
	s := NewSigner(cfg.S3)
	if s == nil || db == nil || rdb == nil {
		return nil
	}
	return &Syncer{signer: s, prefix: cfg.Prefix, exts: cfg.AudioExtensions, interval: cfg.AudioSyncInterval,
		client: &http.Client{Timeout: listPageTimeout}, db: db, kv: redisSyncKV{rdb}, log: log, now: time.Now}
}

// Interval is LISTEN_AUDIO_SYNC_INTERVAL (0 = no scheduled syncs).
func (s *Syncer) Interval() time.Duration { return s.interval }

// Run syncs about 30 s after start, then every interval, until ctx ends.
// Only the replica that takes the Redis lock syncs. A nil Syncer or a zero
// interval returns at once.
func (s *Syncer) Run(ctx context.Context) error {
	if s == nil || s.interval <= 0 {
		return nil
	}
	t := time.NewTimer(syncFirstDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		if _, err := s.locked(ctx, syncRunTimeout); err != nil && !errors.Is(err, ErrSyncRunning) {
			s.log.WarnContext(ctx, "audio sync failed", logging.Err(err))
		}
		t.Reset(s.interval)
	}
}

// SyncNow runs a sync at once (at most 60 s). ErrSyncRunning when a sync
// holds the lock; ErrSyncCooldown within 30 s of the last SyncNow.
func (s *Syncer) SyncNow(ctx context.Context) (SyncResult, error) {
	ok, err := s.kv.setNX(ctx, redisx.KeyListenAudioSyncCooldown, syncCooldown)
	if err != nil {
		return SyncResult{}, fmt.Errorf("audio sync cooldown: %w", err)
	}
	if !ok {
		// A running sync answers "running" rather than "too soon".
		if running, _ := s.kv.get(ctx, redisx.KeyListenAudioSyncLock); running != nil {
			return SyncResult{}, ErrSyncRunning
		}
		return SyncResult{}, ErrSyncCooldown
	}
	return s.locked(ctx, syncNowTimeout)
}

// Status returns the last sync, or false when none was recorded.
func (s *Syncer) Status(ctx context.Context) (SyncStatus, bool) {
	raw, err := s.kv.get(ctx, redisx.KeyListenAudioSyncStatus)
	var st SyncStatus
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return SyncStatus{}, false
	}
	return st, true
}

// locked takes the lock and syncs within timeout.
func (s *Syncer) locked(parent context.Context, timeout time.Duration) (SyncResult, error) {
	ok, err := s.kv.setNX(parent, redisx.KeyListenAudioSyncLock, syncLockTTL)
	if err != nil {
		return SyncResult{}, fmt.Errorf("audio sync lock: %w", err)
	}
	if !ok {
		return SyncResult{}, ErrSyncRunning
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	defer cancel()
	defer func() { _ = s.kv.del(context.WithoutCancel(parent), redisx.KeyListenAudioSyncLock) }()
	return s.sync(ctx)
}

// sync lists the whole bucket prefix and replaces the track_audio rows. A
// failed or partial listing changes no rows.
func (s *Syncer) sync(ctx context.Context) (SyncResult, error) {
	start := s.now()
	prev, _ := s.Status(ctx)
	files, err := s.listAll(ctx)
	var added, removed []string
	if err == nil {
		added, removed, err = s.db.ReplaceTrackAudio(ctx, files)
	}
	at := start.UTC().Truncate(time.Second)
	st := SyncStatus{LastSyncAt: &at, Files: prev.Files}
	if err != nil {
		st.LastError = err.Error()
		s.saveStatus(ctx, st)
		return SyncResult{}, err
	}
	st.Files = len(files)
	s.saveStatus(ctx, st)
	// Found files may have a cached "no file" answer, removed ones a cached
	// "found" one: drop both.
	keys := make([]string, 0, len(files)+len(removed))
	for _, f := range files {
		keys = append(keys, redisx.KeyListenAudioPrefix+f.TrackID)
	}
	for _, id := range removed {
		keys = append(keys, redisx.KeyListenAudioPrefix+id)
	}
	for i := 0; i < len(keys); i += cacheDelBatch {
		if err := s.kv.del(ctx, keys[i:min(i+cacheDelBatch, len(keys))]...); err != nil {
			s.log.WarnContext(ctx, "audio sync: clearing the lookup cache failed", logging.Err(err))
			break
		}
	}
	res := SyncResult{Files: len(files), Added: len(added), Removed: len(removed), TookMS: s.now().Sub(start).Milliseconds()}
	s.log.InfoContext(ctx, "audio sync done", slog.Int("files", res.Files), slog.Int("added", res.Added),
		slog.Int("removed", res.Removed), slog.Int64("took_ms", res.TookMS))
	return res, nil
}

func (s *Syncer) saveStatus(ctx context.Context, st SyncStatus) {
	if raw, err := json.Marshal(st); err == nil {
		_ = s.kv.set(ctx, redisx.KeyListenAudioSyncStatus, raw)
	}
}

// listAll pages through ListObjectsV2 and returns the audio files, one per
// track (the first extension in LISTEN_AUDIO_EXTENSIONS order).
func (s *Syncer) listAll(ctx context.Context) ([]store.TrackAudio, error) {
	var objects []listObject
	token := ""
	for page := 0; ; page++ {
		if page == syncMaxPages {
			return nil, fmt.Errorf("audio sync: more than %d pages", syncMaxPages)
		}
		res, err := s.listPage(ctx, token)
		if err != nil {
			return nil, err
		}
		objects = append(objects, res.Contents...)
		if !res.IsTruncated {
			break
		}
		if res.NextContinuationToken == "" {
			return nil, errors.New("audio sync: truncated listing without a continuation token")
		}
		token = res.NextContinuationToken
	}
	return pickFiles(objects, s.prefix, s.exts), nil
}

type listObject struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

type listResult struct {
	IsTruncated           bool         `xml:"IsTruncated"`
	NextContinuationToken string       `xml:"NextContinuationToken"`
	Contents              []listObject `xml:"Contents"`
}

// listPage fetches one ListObjectsV2 page. Errors never include the URL.
func (s *Syncer) listPage(ctx context.Context, token string) (listResult, error) {
	params := map[string]string{"list-type": "2", "prefix": s.prefix, "max-keys": "1000"}
	if token != "" {
		params["continuation-token"] = token
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.signer.PresignBucket(http.MethodGet, params, time.Minute, s.now()), nil)
	if err != nil {
		return listResult{}, errors.New("audio sync: bad storage URL")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return listResult{}, fmt.Errorf("audio sync: %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return listResult{}, fmt.Errorf("audio sync: storage answered %d", resp.StatusCode)
	}
	return parseListPage(io.LimitReader(resp.Body, 32<<20))
}

func parseListPage(r io.Reader) (listResult, error) {
	var res listResult
	if err := xml.NewDecoder(r).Decode(&res); err != nil {
		return listResult{}, fmt.Errorf("audio sync: bad listing: %w", err)
	}
	return res, nil
}

var audioKey = regexp.MustCompile(`^([0-9A-Za-z]{22})\.([a-z0-9]{1,8})$`)

// pickFiles keeps the keys {prefix}{track_id}.{ext} with an allowed
// extension, one per track: the first extension in exts order.
func pickFiles(objects []listObject, prefix string, exts []string) []store.TrackAudio {
	rank := make(map[string]int, len(exts))
	for i, e := range exts {
		rank[e] = i
	}
	best := map[string]listObject{}
	bestExt := map[string]string{}
	var order []string
	for _, o := range objects {
		rest, ok := strings.CutPrefix(o.Key, prefix)
		if !ok {
			continue
		}
		m := audioKey.FindStringSubmatch(rest)
		if m == nil {
			continue
		}
		id, ext := m[1], m[2]
		r, ok := rank[ext]
		if !ok {
			continue
		}
		if cur, seen := bestExt[id]; seen && rank[cur] <= r {
			continue
		} else if !seen {
			order = append(order, id)
		}
		best[id], bestExt[id] = o, ext
	}
	out := make([]store.TrackAudio, 0, len(order))
	for _, id := range order {
		o, ext := best[id], bestExt[id]
		ctype := audioType("", ext)
		a := store.TrackAudio{TrackID: id, Ext: ext, SizeBytes: max(0, o.Size), ContentType: &ctype}
		if etag := strings.Trim(o.ETag, `"`); etag != "" {
			a.ETag = &etag
		}
		if t, err := time.Parse(time.RFC3339Nano, o.LastModified); err == nil {
			t = t.UTC()
			a.LastModified = &t
		}
		out = append(out, a)
	}
	return out
}
