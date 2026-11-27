package store

import (
	"context"
	"fmt"
)

// LiveTrack is the short form of a track for the admin "Listening now" list.
type LiveTrack struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Artists []ListenPlayArtist `json:"artists"`
}

// ListenerStatuses returns the status of each listener row in ids (rows
// that do not exist are missing from the map).
func (q *Queries) ListenerStatuses(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.db.Query(ctx, `SELECT id, status FROM listeners WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("listener statuses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st string
		if err := rows.Scan(&id, &st); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

// LiveTracks returns the name and artists of each track in ids that is in
// the catalog.
func (q *Queries) LiveTracks(ctx context.Context, ids []string) (map[string]LiveTrack, error) {
	out := make(map[string]LiveTrack, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.db.Query(ctx, `SELECT id, name FROM tracks WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("live tracks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t LiveTrack
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	artists, err := q.artistsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, t := range out {
		t.Artists = make([]ListenPlayArtist, 0, len(artists[id]))
		for _, a := range artists[id] {
			t.Artists = append(t.Artists, ListenPlayArtist{ID: a.ID, Name: a.Name})
		}
		out[id] = t
	}
	return out, nil
}
