package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Listen-along history rules.
const (
	// ListenRejoinWindow: a report for the same song within this time of the
	// last one extends that play instead of starting a new one.
	ListenRejoinWindow = 2 * time.Minute
	// ListenLateWindow: a report for a song that is no longer playing still
	// extends its play when heard within this time.
	ListenLateWindow = 30 * time.Second
	// ListenCountMS is how much of a song must be heard to count (or half
	// the song when it is shorter than twice this).
	ListenCountMS = 30_000
	// listenSlackMS is how much a report may exceed the time since the last
	// one (network delay, timer drift).
	listenSlackMS = 5_000
	// listenCapSlackMS: a play never holds more than the time since it
	// started plus this (one report's worth), however many reports come.
	listenCapSlackMS = 20_000
)

// ErrBadCursor means a history cursor could not be decoded.
var ErrBadCursor = errors.New("invalid cursor")

// ListenPlay is one counted play in a listener's history.
type ListenPlay struct {
	ID          int64           `json:"id"`
	ListenedAt  time.Time       `json:"listened_at"`
	LastHeardAt time.Time       `json:"last_heard_at"`
	HeardMS     int             `json:"heard_ms"`
	Track       ListenPlayTrack `json:"track"`
}

// ListenPlayTrack is the track of a ListenPlay.
type ListenPlayTrack struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	DurationMS    int32              `json:"duration_ms"`
	Explicit      bool               `json:"explicit"`
	ExternalURL   string             `json:"external_url,omitempty"`
	Artists       []ListenPlayArtist `json:"artists"`
	AlbumName     string             `json:"album_name"`
	CoverImageURL string             `json:"cover_image_url"`
}

// ListenPlayArtist is an artist of a ListenPlayTrack.
type ListenPlayArtist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// listenThreshold is the heard time that makes a play count.
func listenThreshold(durationMS int64) int64 {
	if durationMS <= 0 {
		return ListenCountMS
	}
	return min(ListenCountMS, durationMS/2)
}

// RecordListenerProgress adds deltaMS of heard audio to the listener's
// current play of trackID: the play heard within the last two minutes, else
// a new one. An extension adds at most the time since the last report plus
// 5 s, and a play never holds more than the time since it started plus 20 s,
// so repeated reports cannot inflate it. The play counts once heard_ms
// reaches 30 s, or half of a song shorter than a minute. Reports of one
// listener are serialized (the listener row is locked), so concurrent
// reports never double count. A track missing from the catalog and a
// revoked listener are ignored.
func (s *Store) RecordListenerProgress(ctx context.Context, listenerID int64, trackID string, deltaMS, durationMS int64) error {
	return s.recordProgress(ctx, listenerID, trackID, deltaMS, &durationMS, ListenRejoinWindow, true)
}

// ExtendListenerProgress adds a late report for a song that is no longer
// playing (the last flush arrives just after the track changed): it extends
// the listener's play of trackID last reported as current within 30 s, with
// the same clamp, cap and count rule, and never starts a new play. It keeps
// last_heard_at, so late reports cannot keep a skipped song open. The song's
// length comes from the catalog.
func (s *Store) ExtendListenerProgress(ctx context.Context, listenerID int64, trackID string, deltaMS int64) error {
	return s.recordProgress(ctx, listenerID, trackID, deltaMS, nil, ListenLateWindow, false)
}

// recordProgress extends the play heard within window, or (create) starts
// one. A nil durationMS reads the length from the catalog.
func (s *Store) recordProgress(ctx context.Context, listenerID int64, trackID string, deltaMS int64,
	durationMS *int64, window time.Duration, create bool) error {
	if deltaMS <= 0 {
		return nil
	}
	return s.InTx(ctx, func(q *Queries) error {
		var one int
		err := q.db.QueryRow(ctx, `SELECT 1 FROM listeners WHERE id = $1 AND status <> 'revoked' FOR UPDATE`, listenerID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock listener: %w", err)
		}
		if durationMS == nil {
			var d int64
			err := q.db.QueryRow(ctx, `SELECT duration_ms FROM tracks WHERE id = $1`, trackID).Scan(&d)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("track duration: %w", err)
			}
			durationMS = &d
		}
		threshold := listenThreshold(*durationMS)
		// The new total: the clamped delta, capped at the time since the play
		// started plus 20 s, never less than before. A late report (create is
		// false) keeps last_heard_at, so its window does not slide.
		tag, err := q.db.Exec(ctx, `
			UPDATE listener_plays p SET heard_ms = n.heard, counted = p.counted OR n.heard >= $4::bigint,
				last_heard_at = CASE WHEN $7::boolean THEN now() ELSE p.last_heard_at END
			FROM (
				SELECT id, GREATEST(heard_ms::bigint, LEAST(
					heard_ms + LEAST($3::bigint, GREATEST(0, (EXTRACT(EPOCH FROM now() - last_heard_at) * 1000)::bigint) + $5::bigint),
					GREATEST(0, (EXTRACT(EPOCH FROM now() - started_at) * 1000)::bigint) + $8::bigint)) AS heard
				FROM listener_plays
				WHERE listener_id = $1 AND track_id = $2 AND last_heard_at > now() - $6::bigint * interval '1 second'
				ORDER BY last_heard_at DESC LIMIT 1
			) n
			WHERE p.id = n.id`,
			listenerID, trackID, deltaMS, threshold, int64(listenSlackMS), int64(window/time.Second), create,
			int64(listenCapSlackMS))
		if err != nil {
			return fmt.Errorf("extend listener play: %w", err)
		}
		if tag.RowsAffected() > 0 || !create {
			return nil
		}
		_, err = q.db.Exec(ctx, `
			INSERT INTO listener_plays (listener_id, track_id, heard_ms, counted)
			SELECT $1, $2, $3::integer, $3::bigint >= $4::bigint
			WHERE EXISTS (SELECT 1 FROM tracks WHERE id = $2)`, listenerID, trackID, deltaMS, threshold)
		if err != nil {
			return fmt.Errorf("insert listener play: %w", err)
		}
		return nil
	})
}

// EncodeListenCursor makes the opaque cursor for the play after (at, id).
func EncodeListenCursor(at time.Time, id int64) string {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + ":" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeListenCursor(c string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, 0, ErrBadCursor
	}
	a, b, ok := strings.Cut(string(raw), ":")
	us, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil || id <= 0 {
		return time.Time{}, 0, ErrBadCursor
	}
	return time.UnixMicro(us), id, nil
}

// ListenerHistory returns a listener's counted plays, newest first, after
// cursor ("" = from the newest), and the cursor of the next page (nil at
// the end). ErrBadCursor for a cursor that does not decode.
func (q *Queries) ListenerHistory(ctx context.Context, listenerID int64, limit int, cursor string) ([]ListenPlay, *string, error) {
	before, beforeID := time.Now().Add(time.Hour), int64(1<<62)
	if cursor != "" {
		var err error
		if before, beforeID, err = decodeListenCursor(cursor); err != nil {
			return nil, nil, err
		}
	}
	rows, err := q.db.Query(ctx, `
		SELECT p.id, p.started_at, p.last_heard_at, p.heard_ms, `+trackColumns+`
		FROM listener_plays p
		JOIN tracks t ON t.id = p.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE p.listener_id = $1 AND p.counted AND (p.started_at, p.id) < ($2, $3)
		ORDER BY p.started_at DESC, p.id DESC
		LIMIT $4`, listenerID, before, beforeID, limit+1)
	if err != nil {
		return nil, nil, fmt.Errorf("listener history: %w", err)
	}
	defer rows.Close()
	type row struct {
		play ListenPlay
		tv   domain.TrackView
	}
	var got []row
	for rows.Next() {
		var r row
		var al albumScan
		if err := rows.Scan(append(append([]any{&r.play.ID, &r.play.ListenedAt, &r.play.LastHeardAt, &r.play.HeardMS},
			scanTrack(&r.tv)...), al.targets()...)...); err != nil {
			return nil, nil, err
		}
		r.tv.Album = al.album()
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if len(got) > limit {
		got = got[:limit]
		c := EncodeListenCursor(got[limit-1].play.ListenedAt, got[limit-1].play.ID)
		next = &c
	}
	views := make([]*domain.TrackView, len(got))
	for i := range got {
		views[i] = &got[i].tv
	}
	if err := q.hydrate(ctx, views...); err != nil {
		return nil, nil, err
	}
	out := make([]ListenPlay, len(got))
	for i, r := range got {
		p := r.play
		p.ListenedAt, p.LastHeardAt = p.ListenedAt.UTC(), p.LastHeardAt.UTC()
		p.Track = ListenPlayTrack{ID: r.tv.ID, Name: r.tv.Name, DurationMS: r.tv.DurationMS, Explicit: r.tv.Explicit,
			ExternalURL: r.tv.ExternalURL, Artists: make([]ListenPlayArtist, 0, len(r.tv.Artists))}
		for _, a := range r.tv.Artists {
			p.Track.Artists = append(p.Track.Artists, ListenPlayArtist{ID: a.ID, Name: a.Name})
		}
		if r.tv.Album != nil {
			p.Track.AlbumName, p.Track.CoverImageURL = r.tv.Album.Name, r.tv.Album.CoverImageURL
		}
		out[i] = p
	}
	return out, next, nil
}
