package main

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"

	"github.com/baoopn/spotify-tracker/internal/admin"
	"github.com/baoopn/spotify-tracker/internal/config"
	"github.com/baoopn/spotify-tracker/internal/lrclib"
	"github.com/baoopn/spotify-tracker/internal/lyrics"
	"github.com/baoopn/spotify-tracker/internal/store"
	"github.com/baoopn/spotify-tracker/internal/tracker"
	"github.com/baoopn/spotify-tracker/internal/translate"
)

// newLyricsQueue builds the lyrics fetcher and the translator (either may be
// nil when disabled) and hooks them together.
func newLyricsQueue(cfg config.API, st *store.Store, svc *tracker.Service, adminAPI *admin.API,
	log *slog.Logger) (*lyrics.Refresher, *translate.Worker) {
	lr, tr := newLyricsRefresher(cfg, st, log), newTranslator(cfg, st, log)
	hookLyricsQueue(svc, lr, tr, adminAPI, cfg)
	return lr, tr
}

// newLyricsRefresher builds the LRCLIB lyrics fetcher, or nil when
// LYRICS_ENABLED is false.
func newLyricsRefresher(cfg config.API, st *store.Store, log *slog.Logger) *lyrics.Refresher {
	if !cfg.Lyrics.Enabled {
		log.Info("lyrics fetcher disabled (LYRICS_ENABLED=false)")
		return nil
	}
	r := lyrics.New(st, lrclib.New(cfg.Lyrics.APIURL, cfg.Lyrics.Spacing, nil), workerID(),
		log.With(slog.String("component", "lyrics")))
	r.Interval, r.Batch, r.NotFoundRetry = cfg.Lyrics.Interval, cfg.Lyrics.Batch, cfg.Lyrics.NotFoundRetry
	r.Lease = cfg.Lyrics.Lease
	return r
}

// newTranslator builds the Claude lyrics translator, or nil when
// TRANSLATE_ENABLED is false.
func newTranslator(cfg config.API, st *store.Store, log *slog.Logger) *translate.Worker {
	t := cfg.Translate
	if !t.Enabled {
		log.Info("lyrics translation disabled (TRANSLATE_ENABLED=false)")
		return nil
	}
	w := translate.New(st, translate.NewClaude(t.APIKey, t.Model, t.Effort), workerID(),
		log.With(slog.String("component", "translate")))
	w.Interval, w.Batch, w.Spacing, w.Lease = t.Interval, t.Batch, t.Spacing, t.Lease
	w.RecentWindow, w.DailyCap, w.MonthlyCap = t.RecentWindow, t.DailyCap, t.MonthlyCap
	w.MaxAttempts = int32(t.MaxAttempts)
	log.Info("lyrics translation enabled", slog.String("model", t.Model), slog.Int("daily_cap", t.DailyCap),
		slog.Int("monthly_cap", t.MonthlyCap))
	return w
}

// hookLyricsQueue wakes the lyrics fetcher and the translator for the song
// playing now, starts a translation once lyrics are found, and lets the
// admin re-queues nudge the fetcher and the translator. Either worker may be
// nil.
func hookLyricsQueue(svc *tracker.Service, lr *lyrics.Refresher, tr *translate.Worker, adminAPI *admin.API, cfg config.API) {
	adminAPI.LyricsLookup = lr != nil
	if lr != nil {
		adminAPI.LyricsNudge = lr.Nudge
	}
	adminAPI.Translation = admin.TranslationInfo{Enabled: tr != nil, DailyCap: cfg.Translate.DailyCap,
		MonthlyCap: cfg.Translate.MonthlyCap, Model: cfg.Translate.Model}
	if tr != nil {
		adminAPI.Translation.Nudge = tr.Nudge
	}
	if lr != nil && tr != nil {
		lr.OnFound = tr.LyricsFound
	}
	if lr == nil && tr == nil {
		return
	}
	svc.OnPlaying(func(trackID string) {
		if lr != nil {
			lr.Wake(trackID)
		}
		if tr != nil {
			tr.Wake(trackID)
		}
	})
}

// workerID names this process as a queue lease owner.
func workerID() string {
	host, _ := os.Hostname()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return host + "-" + hex.EncodeToString(b)
}
