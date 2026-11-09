package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

// ─── fakes ────────────────────────────────────────────────────────────────────

type fakeRunner struct {
	runErr  error
	runs    int
	genRec  domain.TopPlaylistRecord
	genErr  error
	genCall []string // "period start"
	regCall []string // Regenerate calls, "period start"
	verErr  error
	verifs  int
	owner   [2]string // name, source
}

func (f *fakeRunner) Verify(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("Verify without a deadline")
	}
	f.verifs++
	return f.verErr
}

func (f *fakeRunner) Owner() (string, string) {
	if f.owner[1] == "" {
		return "", topplaylists.OwnerSourceNone
	}
	return f.owner[0], f.owner[1]
}

func (f *fakeRunner) RunNow(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("RunNow without a deadline")
	}
	f.runs++
	return f.runErr
}

func (f *fakeRunner) Generate(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error) {
	if _, ok := ctx.Deadline(); !ok {
		return domain.TopPlaylistRecord{}, errors.New("Generate without a deadline")
	}
	f.genCall = append(f.genCall, string(p)+" "+start.UTC().Format(time.RFC3339))
	return f.genRec, f.genErr
}

func (f *fakeRunner) Regenerate(ctx context.Context, p topplaylists.Period, start time.Time) (domain.TopPlaylistRecord, error) {
	if _, ok := ctx.Deadline(); !ok {
		return domain.TopPlaylistRecord{}, errors.New("Regenerate without a deadline")
	}
	f.regCall = append(f.regCall, string(p)+" "+start.UTC().Format(time.RFC3339))
	return f.genRec, f.genErr
}

type fakeTops struct {
	rows       []domain.TopPlaylistRecord
	listArgs   string
	tracks     []domain.TopPlaylistTrack
	hiddenArgs string
}

func (f *fakeTops) find(period string, start time.Time) (domain.TopPlaylistRecord, bool) {
	for _, r := range f.rows {
		if r.Period == period && r.PeriodStart.Equal(start) {
			return r, true
		}
	}
	return domain.TopPlaylistRecord{}, false
}

func (f *fakeTops) AdminTopPlaylists(_ context.Context, period string, limit, offset int) ([]domain.TopPlaylistRecord, int, error) {
	f.listArgs = fmt.Sprintf("%q %d %d", period, limit, offset)
	return f.rows, len(f.rows), nil
}

func (f *fakeTops) TopPlaylist(_ context.Context, period string, start time.Time) (domain.TopPlaylistRecord, error) {
	if r, ok := f.find(period, start); ok {
		return r, nil
	}
	return domain.TopPlaylistRecord{}, domain.ErrNotFound
}

func (f *fakeTops) TopPlaylistTracks(context.Context, string, time.Time) ([]domain.TopPlaylistTrack, error) {
	return f.tracks, nil
}

func (f *fakeTops) SetTopPlaylistHidden(_ context.Context, period string, start time.Time, hidden bool) (domain.TopPlaylistRecord, error) {
	f.hiddenArgs = fmt.Sprintf("%s %s %v", period, start.UTC().Format(time.RFC3339), hidden)
	r, ok := f.find(period, start)
	if !ok {
		return r, domain.ErrNotFound
	}
	r.Hidden = hidden
	return r, nil
}

type auditCall struct {
	actor, action string
	detail        map[string]any
}

type fakeAuditor struct{ calls []auditCall }

func (f *fakeAuditor) Audit(_ context.Context, actor, action string, detail any, _ string) error {
	b, _ := json.Marshal(detail)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.calls = append(f.calls, auditCall{actor, action, m})
	return nil
}
