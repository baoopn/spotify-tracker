// Package importer copies the recorded data from the legacy Directus
// instance into PostgreSQL. It is idempotent: re-running it upserts catalog
// rows and skips history rows that already exist.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// Stats summarises an import run.
type Stats struct {
	Artists, Albums, Tracks, Features, History, HistorySkipped int
	CurrentPlayback                                            bool
}

// Importer reads from Directus and writes to the store.
type Importer struct {
	base     string
	tokens   []string
	pageSize int
	http     *http.Client
	store    *store.Store
	log      *slog.Logger
	dryRun   bool
}

// New creates an Importer. tokens is a comma-separated list of Directus
// tokens; for each request they are tried in order until one is authorized,
// so the legacy per-worker tokens (which each read only some collections)
// can be combined instead of requiring an admin token.
func New(directusURL, tokens string, pageSize int, st *store.Store, log *slog.Logger, dryRun bool) *Importer {
	base := strings.TrimRight(directusURL, "/")
	base = strings.TrimSuffix(base, "/items")
	var toks []string
	for _, t := range strings.Split(tokens, ",") {
		if t = strings.TrimSpace(t); t != "" {
			toks = append(toks, t)
		}
	}
	return &Importer{base: base, tokens: toks, pageSize: pageSize, store: st, log: log, dryRun: dryRun,
		http: &http.Client{Timeout: 60 * time.Second}}
}

// Run imports every collection in dependency order.
func (im *Importer) Run(ctx context.Context) (Stats, error) {
	var st Stats
	steps := []struct {
		name string
		fn   func(context.Context, *Stats) error
	}{
		{"artists", im.importArtists},
		{"albums", im.importAlbums},
		{"tracks", im.importTracks},
		{"track_features", im.importFeatures},
		{"listening_history", im.importHistory},
		{"current_listening", im.importCurrent},
	}
	for _, s := range steps {
		start := time.Now()
		if err := s.fn(ctx, &st); err != nil {
			return st, fmt.Errorf("import %s: %w", s.name, err)
		}
		im.log.Info("collection imported", slog.String("collection", s.name), slog.Duration("took", time.Since(start)))
	}
	return st, nil
}

// ─── Directus access ──────────────────────────────────────────────────────────

func (im *Importer) get(ctx context.Context, path string, q url.Values, out any) error {
	u := im.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var lastErr error
	for _, tok := range im.tokens {
		status, body, err := im.do(ctx, u, tok)
		if err != nil {
			return err
		}
		if status == http.StatusOK {
			return json.Unmarshal(body, out)
		}
		msg := string(body)
		if len(msg) > 500 {
			msg = msg[:500]
		}
		lastErr = fmt.Errorf("directus GET %s: %d: %s", path, status, msg)
		if status != http.StatusUnauthorized && status != http.StatusForbidden {
			return lastErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no Directus token configured")
	}
	return lastErr
}

func (im *Importer) do(ctx context.Context, u, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := im.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	return resp.StatusCode, body, err
}

// pages iterates a collection page by page.
func pages[T any](ctx context.Context, im *Importer, collection, fields, sort string, fn func([]T) error) error {
	return pagesWith(ctx, im, collection, fields, sort, nil, fn)
}

// pagesWith is pages with extra query parameters (e.g. Directus filters).
func pagesWith[T any](ctx context.Context, im *Importer, collection, fields, sort string, extra url.Values, fn func([]T) error) error {
	for page := 1; ; page++ {
		q := url.Values{
			"limit":  {strconv.Itoa(im.pageSize)},
			"page":   {strconv.Itoa(page)},
			"fields": {fields},
		}
		for k, v := range extra {
			q[k] = v
		}
		if sort != "" {
			q.Set("sort", sort)
		}
		var resp struct {
			Data []T `json:"data"`
		}
		if err := im.get(ctx, "/items/"+collection, q, &resp); err != nil {
			return err
		}
		if len(resp.Data) == 0 {
			return nil
		}
		if err := fn(resp.Data); err != nil {
			return err
		}
		if len(resp.Data) < im.pageSize {
			return nil
		}
	}
}

func (im *Importer) tx(ctx context.Context, fn func(q *store.Queries) error) error {
	if im.dryRun {
		return nil
	}
	return im.store.InTx(ctx, fn)
}

// ─── Collections ──────────────────────────────────────────────────────────────

type dArtist struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	URI         *string `json:"uri"`
	ExternalURL *string `json:"external_url"`
}

func (im *Importer) importArtists(ctx context.Context, st *Stats) error {
	return pages(ctx, im, "artists", "id,name,uri,external_url", "id", func(items []dArtist) error {
		err := im.tx(ctx, func(q *store.Queries) error {
			for _, a := range items {
				if err := q.UpsertArtist(ctx, domain.Artist{ID: a.ID, Name: a.Name, URI: str(a.URI), ExternalURL: str(a.ExternalURL)}); err != nil {
					return err
				}
			}
			return nil
		})
		st.Artists += len(items)
		return err
	})
}

type dAlbum struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	ReleaseDate *string `json:"release_date"`
	CoverImage  *string `json:"cover_image"`
	URI         *string `json:"uri"`
	ExternalURL *string `json:"external_url"`
}

func (im *Importer) importAlbums(ctx context.Context, st *Stats) error {
	return pages(ctx, im, "albums", "id,name,release_date,cover_image,uri,external_url", "id", func(items []dAlbum) error {
		err := im.tx(ctx, func(q *store.Queries) error {
			for _, a := range items {
				if err := q.UpsertAlbum(ctx, domain.Album{
					ID: a.ID, Name: a.Name, ReleaseDate: str(a.ReleaseDate), CoverImageURL: str(a.CoverImage),
					URI: str(a.URI), ExternalURL: str(a.ExternalURL),
				}); err != nil {
					return err
				}
			}
			return nil
		})
		st.Albums += len(items)
		return err
	})
}

type dTrack struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	URI         *string           `json:"uri"`
	ISRC        *string           `json:"isrc"`
	DurationMS  flexInt           `json:"duration_ms"`
	Explicit    *bool             `json:"explicit"`
	ExternalURL *string           `json:"external_url"`
	AlbumID     json.RawMessage   `json:"album_id"`
	Artists     []json.RawMessage `json:"artists"`
}

func (im *Importer) importTracks(ctx context.Context, st *Stats) error {
	return pages(ctx, im, "tracks", "*,artists.*", "id", func(items []dTrack) error {
		err := im.tx(ctx, func(q *store.Queries) error {
			for _, t := range items {
				artistIDs := junctionIDs(t.Artists)
				if err := q.UpsertTrack(ctx, domain.Track{
					ID: t.ID, Name: t.Name, URI: str(t.URI), ISRC: str(t.ISRC),
					DurationMS: int32(t.DurationMS), Explicit: t.Explicit != nil && *t.Explicit,
					ExternalURL: str(t.ExternalURL), AlbumID: relationID(t.AlbumID),
				}, artistIDs); err != nil {
					return err
				}
			}
			return nil
		})
		st.Tracks += len(items)
		return err
	})
}

type dFeatures struct {
	TrackID          json.RawMessage `json:"track_id"`
	ISRC             *string         `json:"isrc"`
	Acousticness     *flexFloat      `json:"acousticness"`
	Danceability     *flexFloat      `json:"danceability"`
	Energy           *flexFloat      `json:"energy"`
	Instrumentalness *flexFloat      `json:"instrumentalness"`
	Key              *flexInt        `json:"key"`
	Liveness         *flexFloat      `json:"liveness"`
	Loudness         *flexFloat      `json:"loudness"`
	Mode             *flexInt        `json:"mode"`
	Speechiness      *flexFloat      `json:"speechiness"`
	Tempo            *flexFloat      `json:"tempo"`
	Valence          *flexFloat      `json:"valence"`
	Genres           json.RawMessage `json:"genres"`
	Enriched         *bool           `json:"enriched"`
}

func (im *Importer) importFeatures(ctx context.Context, st *Stats) error {
	return pages(ctx, im, "track_features", "*", "id", func(items []dFeatures) error {
		err := im.tx(ctx, func(q *store.Queries) error {
			for _, f := range items {
				trackID := relationID(f.TrackID)
				if trackID == "" {
					continue
				}
				if _, err := q.EnsureTrackFeatures(ctx, trackID, str(f.ISRC)); err != nil {
					return err
				}
				if f.Enriched == nil || !*f.Enriched {
					continue
				}
				genres := parseGenres(f.Genres)
				af := domain.AudioFeatures{
					Acousticness: f.Acousticness.ptr(), Danceability: f.Danceability.ptr(),
					Energy: f.Energy.ptr(), Instrumentalness: f.Instrumentalness.ptr(),
					Key: f.Key.ptr32(), Liveness: f.Liveness.ptr(), Loudness: f.Loudness.ptr(),
					Mode: f.Mode.ptr32(), Speechiness: f.Speechiness.ptr(), Tempo: f.Tempo.ptr(),
					Valence: f.Valence.ptr(),
				}
				found := af.Energy != nil || af.Tempo != nil || af.Danceability != nil || len(genres) > 0
				if err := q.SaveFeatures(ctx, trackID, found, af, genres, "soundcharts"); err != nil {
					return err
				}
			}
			return nil
		})
		st.Features += len(items)
		return err
	})
}

type dHistory struct {
	TrackID     json.RawMessage `json:"track_id"`
	PlayedAt    string          `json:"played_at"`
	UpdatedAt   *string         `json:"updated_at"`
	DurationMS  *flexInt        `json:"duration_ms"`
	ReplayCount *flexInt        `json:"replay_count"`
}

func (im *Importer) importHistory(ctx context.Context, st *Stats) error {
	return pages(ctx, im, "listening_history", "track_id,played_at,updated_at,duration_ms,replay_count", "played_at",
		func(items []dHistory) error {
			created, skipped, err := im.insertHistoryPage(ctx, items)
			st.History += created
			st.HistorySkipped += skipped
			return err
		})
}

// historyRow is a legacy history item converted to insert arguments.
type historyRow struct {
	trackID             string
	playedAt, updatedAt time.Time
	durationMS, replays int32
}

// toHistoryRow converts a Directus item; ok is false for unusable rows
// (no track or unparseable played_at).
func toHistoryRow(h dHistory) (historyRow, bool) {
	trackID := relationID(h.TrackID)
	playedAt, err := parseTime(h.PlayedAt)
	if trackID == "" || err != nil {
		return historyRow{}, false
	}
	row := historyRow{trackID: trackID, playedAt: playedAt, updatedAt: playedAt, replays: 1}
	if h.UpdatedAt != nil {
		if t, err := parseTime(*h.UpdatedAt); err == nil {
			row.updatedAt = t
		}
	}
	if h.ReplayCount != nil && *h.ReplayCount > 0 {
		row.replays = int32(*h.ReplayCount)
	}
	if h.DurationMS != nil {
		row.durationMS = int32(*h.DurationMS)
	}
	return row, true
}

// insertHistoryPage inserts one page in a transaction and counts created
// and skipped (unusable or already present) rows. In dry-run mode every
// item counts as created.
func (im *Importer) insertHistoryPage(ctx context.Context, items []dHistory) (created, skipped int, err error) {
	if im.dryRun {
		return len(items), 0, nil
	}
	err = im.store.InTx(ctx, func(q *store.Queries) error {
		for _, h := range items {
			row, ok := toHistoryRow(h)
			if !ok {
				skipped++
				continue
			}
			inserted, err := q.InsertHistory(ctx, row.trackID, row.playedAt, row.updatedAt, row.durationMS, row.replays, nil)
			if err != nil {
				return err
			}
			if inserted {
				created++
			} else {
				skipped++
			}
		}
		return nil
	})
	return created, skipped, err
}

func (im *Importer) importCurrent(ctx context.Context, st *Stats) error {
	var resp struct {
		Data *struct {
			TrackID    json.RawMessage `json:"track_id"`
			IsPlaying  *bool           `json:"is_playing"`
			ProgressMS *flexInt        `json:"progress_ms"`
			FetchedAt  *string         `json:"fetched_at"`
		} `json:"data"`
	}
	if err := im.get(ctx, "/items/current_listening", nil, &resp); err != nil {
		// Singleton may be missing or not readable with this token.
		im.log.Warn("skipping current_listening", slog.Any("error", err))
		return nil
	}
	d := resp.Data
	if d == nil || relationID(d.TrackID) == "" {
		return nil
	}
	at := time.Now().UTC()
	if d.FetchedAt != nil {
		if t, err := parseTime(*d.FetchedAt); err == nil {
			at = t
		}
	}
	err := im.tx(ctx, func(q *store.Queries) error {
		return q.SetCurrentPlayback(ctx, relationID(d.TrackID), false, d.ProgressMS.ptr32(), at)
	})
	st.CurrentPlayback = err == nil
	return err
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// relationID extracts an id from a Directus relation that may be a raw id
// ("abc") or an expanded object ({"id": "abc"}).
func relationID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return relationID(obj.ID)
	}
	return ""
}

// junctionIDs extracts artist ids from M2M junction rows such as
// {"id": 1, "tracks_id": "t", "artists_id": "a"}.
func junctionIDs(rows []json.RawMessage) []string {
	var ids []string
	seen := map[string]bool{}
	for _, raw := range rows {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			if id := relationID(raw); id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			continue
		}
		for k, v := range m {
			if strings.HasPrefix(k, "artists_id") || k == "artist_id" {
				if id := relationID(v); id != "" && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func parseGenres(raw json.RawMessage) []domain.Genre {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// Directus JSON fields may arrive as a JSON value or as an encoded string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var gs []domain.Genre
	if json.Unmarshal(raw, &gs) != nil {
		return nil
	}
	out := gs[:0]
	for _, g := range gs {
		if g.Root == "" {
			continue
		}
		if g.Sub == nil {
			g.Sub = []string{}
		}
		out = append(out, g)
	}
	return out
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000Z",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.000",
}

func parseTime(s string) (time.Time, error) {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("unparseable time: " + s)
}

// flexFloat accepts JSON numbers and numeric strings (Directus returns
// decimal columns as strings).
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

func (f *flexFloat) ptr() *float64 {
	if f == nil {
		return nil
	}
	v := float64(*f)
	return &v
}

// flexInt accepts JSON numbers and numeric strings.
type flexInt int64

func (i *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*i = flexInt(int64(v))
	return nil
}

func (i *flexInt) ptr32() *int32 {
	if i == nil {
		return nil
	}
	v := int32(*i)
	return &v
}
