package store_test

// Integration tests against a real PostgreSQL for the lyrics lookup queue
// (see translations_pg_test.go for STORE_TEST_DATABASE_URL).

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// bare adds a track with an artist and no lyrics row, played ago (0 = never).
func bare(t *testing.T, pool *pgxpool.Pool, id string, ago time.Duration) {
	t.Helper()
	exec(t, pool, `INSERT INTO artists (id, name) VALUES ('ar', 'Artist') ON CONFLICT DO NOTHING`)
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ($1, 'Song '||$1, 200000)`, id)
	exec(t, pool, `INSERT INTO track_artists (track_id, artist_id) VALUES ($1, 'ar')`, id)
	if ago > 0 {
		exec(t, pool, `INSERT INTO listening_history (track_id, played_at) VALUES ($1, now() - $2::interval)`, id, ago)
	}
}

func ids(cs []domain.LyricsCandidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.TrackID
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func lyricsRow(t *testing.T, pool *pgxpool.Pool, id string) (status string, claimedBy *string, requested bool) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `SELECT status, claimed_by, requested_at IS NOT NULL
		FROM lyrics WHERE track_id = $1`, id).Scan(&status, &claimedBy, &requested)
	if err != nil {
		t.Fatalf("lyrics row %s: %v", id, err)
	}
	return status, claimedBy, requested
}

var found = domain.Lyrics{Status: domain.LyricsFound, LRCLibID: 7, Plain: "một\nba", Synced: "[00:01.00] một\n[00:03.00] ba"}

// Claim order, leases, results under the claim only, expiry, release.
func TestPGLyricsQueueClaims(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	bare(t, pool, "old", 2*time.Hour)
	bare(t, pool, "recent", 5*time.Minute)
	bare(t, pool, "now", 3*time.Hour) // played long ago, but playing now
	bare(t, pool, "next", 0)          // upcoming, never played
	bare(t, pool, "never", 0)         // library backlog
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('noartist', 'x', 1)`)
	bare(t, pool, "retry", time.Minute) // not found, retry due
	exec(t, pool, `INSERT INTO lyrics (track_id, status, attempts, next_attempt_at, fetched_at)
		VALUES ('retry', 'not_found', 1, now() - interval '1 minute', now() - interval '30 days')`)
	bare(t, pool, "later", time.Second) // not found, retry later: not claimable
	exec(t, pool, `INSERT INTO lyrics (track_id, status, attempts, next_attempt_at, fetched_at)
		VALUES ('later', 'not_found', 1, now() + interval '1 day', now())`)

	got, err := st.ClaimLyrics(ctx, "w1", []string{"now", "next"}, 10*time.Minute, 4)
	if err != nil {
		t.Fatal(err)
	}
	// The song playing now, the upcoming one, then by last play.
	if want := []string{"now", "next", "retry", "recent"}; !eq(ids(got), want) {
		t.Fatalf("claim = %v, want %v", ids(got), want)
	}
	if c := got[0]; c.Name != "Song now" || c.Artist != "Artist" || c.DurationMS != 200000 || c.Status != "" {
		t.Errorf("candidate = %+v", c)
	}
	if c := got[2]; c.Status != domain.LyricsNotFound || c.Attempts != 1 {
		t.Errorf("retry candidate = %+v", c)
	}
	if s, by, _ := lyricsRow(t, pool, "now"); s != domain.LyricsPending || by == nil || *by != "w1" {
		t.Errorf("now row = %s %v; want a pending row claimed by w1", s, by)
	}

	// Another worker skips the live leases; the backlog comes last.
	got2, err := st.ClaimLyrics(ctx, "w2", nil, 10*time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"old", "never"}; !eq(ids(got2), want) {
		t.Fatalf("second claim = %v, want %v (noartist and later excluded)", ids(got2), want)
	}

	// Results are written only under the claim.
	if ok, _, err := st.SetLyrics(ctx, "w2", "now", found); err != nil || ok {
		t.Fatalf("set by a non-owner = %v, %v", ok, err)
	}
	if err := st.DeferLyrics(ctx, "w2", "recent", domain.LyricsError, time.Now().Add(time.Hour), "x"); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := lyricsRow(t, pool, "recent"); s != domain.LyricsPending {
		t.Errorf("deferred by a non-owner: status %s", s)
	}
	if ok, _, err := st.SetLyrics(ctx, "w1", "now", found); err != nil || !ok {
		t.Fatalf("set = %v, %v", ok, err)
	}
	if s, by, _ := lyricsRow(t, pool, "now"); s != domain.LyricsFound || by != nil {
		t.Errorf("now = %s, claimed by %v", s, by)
	}
	// Not found keeps the retry rules: attempts per status, the next try.
	if err := st.DeferLyrics(ctx, "w1", "retry", domain.LyricsNotFound, time.Now().Add(720*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM lyrics WHERE track_id = 'retry' AND claimed_by IS NULL
		AND next_attempt_at > now() + interval '700 hours'`).Scan(&attempts); err != nil || attempts != 2 {
		t.Errorf("retry attempts = %d, %v", attempts, err)
	}
	if err := st.DeferLyrics(ctx, "w1", "next", domain.LyricsError, time.Now().Add(time.Hour), "boom"); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := lyricsRow(t, pool, "next"); s != domain.LyricsError {
		t.Errorf("next = %s", s)
	}

	// An expired lease can be taken again.
	exec(t, pool, `UPDATE lyrics SET claimed_until = now() - interval '1 second' WHERE track_id = 'recent'`)
	got3, err := st.ClaimLyrics(ctx, "w3", nil, 10*time.Minute, 10)
	if err != nil || !eq(ids(got3), []string{"recent"}) {
		t.Fatalf("reclaim = %v, %v", ids(got3), err)
	}
	if ok, _, _ := st.SetLyrics(ctx, "w1", "recent", found); ok {
		t.Error("the old owner wrote after losing the lease")
	}

	// Release gives the track back as it was.
	if err := st.ReleaseLyrics(ctx, "w3", "recent"); err != nil {
		t.Fatal(err)
	}
	if s, by, _ := lyricsRow(t, pool, "recent"); s != domain.LyricsPending || by != nil {
		t.Errorf("released = %s %v", s, by)
	}
}

// A re-queue goes first (after the priority songs), whatever the status;
// found lyrics stay served meanwhile; the mark survives a release and a
// re-queue during the claim.
func TestPGLyricsRequeue(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	seed(t, pool, "found", 3*time.Hour)
	bare(t, pool, "fresh", time.Minute)
	bare(t, pool, "nf", 2*time.Hour)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, attempts, next_attempt_at, fetched_at)
		VALUES ('nf', 'not_found', 4, now() + interval '20 days', now())`)

	if ok, err := st.RequeueLyrics(ctx, "unknown"); err != nil || ok {
		t.Fatalf("unknown = %v, %v", ok, err)
	}
	for _, id := range []string{"nf", "found"} { // found is the newest request
		if ok, err := st.RequeueLyrics(ctx, id); err != nil || !ok {
			t.Fatalf("requeue %s = %v, %v", id, ok, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s, _, req := lyricsRow(t, pool, "found"); s != domain.LyricsFound || !req {
		t.Errorf("found after re-queue = %s, requested %v", s, req)
	}
	now, err := st.NowLyrics(ctx, "found", store.TranslationCaps{Daily: 1})
	if err != nil || now.Status != domain.LyricsFound || now.Plain == "" {
		t.Errorf("found lyrics while re-queued = %+v, %v", now, err)
	}
	tl, err := st.TrackLyrics(ctx, "nf", false, store.TranslationCaps{Daily: 1})
	if err != nil || !tl.Queued || tl.LookingUp || !tl.Waiting || tl.RequestedAt == nil || tl.Attempts != 0 {
		t.Errorf("nf track lyrics = %+v, %v", tl, err)
	}

	got, err := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1)
	if err != nil || !eq(ids(got), []string{"found"}) {
		t.Fatalf("claim = %v, %v; want the newest re-queue first", ids(got), err)
	}
	if got[0].Status != domain.LyricsFound {
		t.Errorf("status = %q", got[0].Status)
	}
	tl, _ = st.TrackLyrics(ctx, "found", false, store.TranslationCaps{Daily: 1})
	if !tl.LookingUp || tl.Queued || tl.RequestedAt == nil {
		t.Errorf("found while claimed = %+v", tl)
	}
	// Released: still first in line.
	if err := st.ReleaseLyrics(ctx, "w1", "found"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 2)
	if !eq(ids(got), []string{"found", "nf"}) {
		t.Fatalf("after release = %v", ids(got))
	}
	// A re-queue while claimed keeps the lease and comes back after the result.
	if ok, _ := st.RequeueLyrics(ctx, "nf"); !ok {
		t.Fatal("requeue while claimed")
	}
	if _, by, _ := lyricsRow(t, pool, "nf"); by == nil || *by != "w1" {
		t.Errorf("lease lost by a re-queue: %v", by)
	}
	if err := st.DeferLyrics(ctx, "w1", "nf", domain.LyricsNotFound, time.Now().Add(720*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	// A found track that is not found on a fresh lookup keeps its lyrics.
	if err := st.DeferLyrics(ctx, "w1", "found", domain.LyricsNotFound, time.Now().Add(720*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := lyricsRow(t, pool, "found"); s != domain.LyricsFound {
		t.Errorf("found after a not-found re-lookup = %s", s)
	}
	got, _ = st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5)
	if !eq(ids(got), []string{"nf", "fresh"}) {
		t.Errorf("re-queued during the claim = %v, want nf again, then fresh", ids(got))
	}
}

// Changed lyrics put an existing translation first in the translation
// queue; unchanged lyrics touch nothing; no translation row is created.
func TestPGLyricsChangeRetranslates(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	caps := store.TranslationCaps{Daily: 10}
	for _, id := range []string{"same", "changed", "other", "untranslated"} {
		seed(t, pool, id, time.Hour)
	}
	exec(t, pool, `UPDATE lyrics SET plain_lyrics = 'một
ba', synced_lyrics = '[00:01.00] một
[00:03.00] ba' WHERE track_id IN ('same', 'changed', 'other', 'untranslated')`)
	for _, id := range []string{"same", "changed", "other"} {
		if _, err := st.EnqueueTranslation(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `UPDATE lyrics_translations SET status = 'done' WHERE track_id IN ('same', 'changed')`)
	exec(t, pool, `UPDATE listening_history SET played_at = now() WHERE track_id = 'other'`) // other: last play wins
	for _, id := range []string{"same", "changed", "untranslated"} {
		if ok, _ := st.RequeueLyrics(ctx, id); !ok {
			t.Fatal(id)
		}
	}
	if got, err := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 3); err != nil || len(got) != 3 {
		t.Fatalf("claim = %v, %v", ids(got), err)
	}

	// Unchanged: no re-translation.
	if ok, re, err := st.SetLyrics(ctx, "w1", "same", found); err != nil || !ok || re {
		t.Fatalf("same = %v %v %v", ok, re, err)
	}
	var requested bool
	_ = pool.QueryRow(ctx, `SELECT requested_at IS NOT NULL FROM lyrics_translations WHERE track_id = 'same'`).Scan(&requested)
	if requested {
		t.Error("unchanged lyrics re-queued the translation")
	}
	// Changed: the translation goes first.
	changed := domain.Lyrics{Status: domain.LyricsFound, Plain: "new words", Synced: "[00:01.00] new words"}
	if ok, re, err := st.SetLyrics(ctx, "w1", "changed", changed); err != nil || !ok || !re {
		t.Fatalf("changed = %v %v %v", ok, re, err)
	}
	// Never translated: no row is created.
	if ok, re, err := st.SetLyrics(ctx, "w1", "untranslated", changed); err != nil || !ok || re {
		t.Fatalf("untranslated = %v %v %v", ok, re, err)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM lyrics_translations WHERE track_id = 'untranslated'`).Scan(&n)
	if n != 0 {
		t.Error("a translation row was created")
	}
	tr, err := st.ClaimTranslations(ctx, "t1", nil, 10*time.Minute, 5)
	if err != nil || len(tr) != 2 || tr[0].TrackID != "changed" || tr[1].TrackID != "other" {
		t.Fatalf("translation claim = %+v, %v; want changed first, then other", tr, err)
	}
	if tr[0].Plain != "new words" {
		t.Errorf("changed candidate = %+v", tr[0])
	}
	if err := st.ReleaseTranslation(ctx, "t1", "changed"); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseTranslation(ctx, "t1", "other"); err != nil {
		t.Fatal(err)
	}

	// Found → instrumental: the translation is no longer served or claimed.
	if ok, _ := st.RequeueLyrics(ctx, "same"); !ok {
		t.Fatal("requeue same")
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1); !eq(ids(got), []string{"same"}) {
		t.Fatalf("claim same = %v", ids(got))
	}
	if ok, re, err := st.SetLyrics(ctx, "w1", "same", domain.Lyrics{Status: domain.LyricsInstrumental}); err != nil || !ok || re {
		t.Fatalf("instrumental = %v %v %v", ok, re, err)
	}
	nl, err := st.NowLyrics(ctx, "same", caps)
	if err != nil || nl.Status != domain.LyricsInstrumental || nl.Translation != nil {
		t.Errorf("instrumental now = %+v, %v", nl, err)
	}
	tl, _ := st.TrackLyrics(ctx, "same", false, caps)
	if tl.Status != domain.LyricsInstrumental || tl.Translation != nil {
		t.Errorf("instrumental track = %+v", tl)
	}
	tr, _ = st.ClaimTranslations(ctx, "t1", nil, 10*time.Minute, 5)
	for _, c := range tr {
		if c.TrackID == "same" {
			t.Error("instrumental lyrics were claimed for translation")
		}
	}
}

// The public now-playing status follows the queue.
func TestPGNowLyricsFollowsQueue(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	caps := store.TranslationCaps{Daily: 1}
	bare(t, pool, "norow", time.Minute)
	bare(t, pool, "pending", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status) VALUES ('pending', 'pending')`)
	bare(t, pool, "nfsoon", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at) VALUES ('nfsoon', 'not_found', now() + interval '1 minute')`)
	bare(t, pool, "nflater", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at) VALUES ('nflater', 'not_found', now() + interval '1 day')`)
	bare(t, pool, "errlater", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at) VALUES ('errlater', 'error', now() + interval '1 day')`)
	bare(t, pool, "leased", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at, claimed_by, claimed_until)
		VALUES ('leased', 'error', now() + interval '1 day', 'w', now() + interval '5 minutes')`)
	for id, want := range map[string]string{"norow": domain.LyricsPending, "pending": domain.LyricsPending,
		"nfsoon": domain.LyricsPending, "nflater": "", "errlater": "", "leased": domain.LyricsPending} {
		got, err := st.NowLyrics(ctx, id, caps)
		if err != nil || got.Status != want {
			t.Errorf("%s = %q, %v; want %q", id, got.Status, err, want)
		}
	}
	if ok, _ := st.RequeueLyrics(ctx, "nflater"); !ok {
		t.Fatal("requeue")
	}
	if got, _ := st.NowLyrics(ctx, "nflater", caps); got.Status != domain.LyricsPending {
		t.Errorf("re-queued not found = %q, want pending", got.Status)
	}
}

func TestPGLyricsQueueSummary(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	seed(t, pool, "f1", time.Hour)
	seed(t, pool, "f2", time.Hour)
	bare(t, pool, "b1", 0)
	bare(t, pool, "b2", time.Minute)
	bare(t, pool, "nf", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at, fetched_at)
		VALUES ('nf', 'not_found', now() + interval '3 days', now() - interval '1 hour')`)
	bare(t, pool, "er", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, next_attempt_at, fetched_at)
		VALUES ('er', 'error', now() + interval '2 hours', now() - interval '2 hours')`)
	bare(t, pool, "inst", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, fetched_at) VALUES ('inst', 'instrumental', now() - interval '3 hours')`)
	if ok, _ := st.RequeueLyrics(ctx, "f2"); !ok {
		t.Fatal("requeue")
	}
	exec(t, pool, `UPDATE lyrics SET claimed_by = 'w', claimed_until = now() + interval '5 minutes' WHERE track_id = 'f1'`)
	bare(t, pool, "p1", 0) // a pending row never looked up counts as backlog
	exec(t, pool, `INSERT INTO lyrics (track_id, status, source) VALUES ('p1', 'pending', 'lrclib')`)

	s, err := st.LyricsQueueSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Queued != 4 || s.Requested != 1 || s.Backlog != 3 || s.InProgress != 1 || s.Found != 0 ||
		s.Instrumental != 1 || s.NotFound != 1 || s.Error != 1 {
		t.Errorf("summary = %+v", s)
	}
	if s.NextRetryAt == nil || time.Until(*s.NextRetryAt) > 3*time.Hour || s.LastLookupAt == nil {
		t.Errorf("next retry %v, last lookup %v", s.NextRetryAt, s.LastLookupAt)
	}
}

// A re-queue survives a worker that crashed while holding it: once the lease
// expires the track is still first in line (lyrics and translations).
func TestPGRequeueSurvivesCrashedClaim(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	seed(t, pool, "asked", 5*time.Hour)
	bare(t, pool, "recent", time.Minute)
	if ok, _ := st.RequeueLyrics(ctx, "asked"); !ok {
		t.Fatal("requeue lyrics")
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1); !eq(ids(got), []string{"asked"}) {
		t.Fatalf("first claim = %v", ids(got))
	}
	// w1 crashes: no release; the lease runs out.
	exec(t, pool, `UPDATE lyrics SET claimed_until = now() - interval '1 second' WHERE track_id = 'asked'`)
	tl, _ := st.TrackLyrics(ctx, "asked", false, store.TranslationCaps{Daily: 1})
	if !tl.Queued || tl.LookingUp || tl.RequestedAt == nil {
		t.Errorf("after the crash = %+v; want queued with the request", tl)
	}
	got, err := st.ClaimLyrics(ctx, "w2", nil, 10*time.Minute, 1)
	if err != nil || !eq(ids(got), []string{"asked"}) {
		t.Fatalf("reclaim = %v, %v; want the re-queued track before the recent play", ids(got), err)
	}
	// The request is still held by the new claim; a result clears it.
	if ok, _, _ := st.SetLyrics(ctx, "w2", "asked", found); !ok {
		t.Fatal("set")
	}
	if _, _, req := lyricsRow(t, pool, "asked"); req {
		t.Error("requested after the result")
	}
	var claimReq bool
	_ = pool.QueryRow(ctx, `SELECT claim_requested_at IS NOT NULL FROM lyrics WHERE track_id = 'asked'`).Scan(&claimReq)
	if claimReq {
		t.Error("claim_requested_at left after the result")
	}

	// Translations: the same.
	seed(t, pool, "tr1", 5*time.Hour)
	seed(t, pool, "tr2", time.Minute)
	for _, id := range []string{"tr1", "tr2"} {
		if _, err := st.EnqueueTranslation(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `UPDATE lyrics_translations SET status = 'done' WHERE track_id IN ('tr1', 'tr2')`)
	if ok, _ := st.RequeueTranslation(ctx, "tr1"); !ok {
		t.Fatal("requeue translation")
	}
	if c, _ := st.ClaimTranslations(ctx, "t1", nil, 10*time.Minute, 1); len(c) != 1 || c[0].TrackID != "tr1" {
		t.Fatalf("translation claim = %+v", c)
	}
	exec(t, pool, `UPDATE lyrics_translations SET claimed_until = now() - interval '1 second' WHERE track_id = 'tr1'`)
	sum, err := st.TranslationQueueSummary(ctx, time.Now())
	if err != nil || sum.Queued < 1 {
		t.Errorf("translation summary = %+v, %v; the crashed re-queue is queued", sum, err)
	}
	c, err := st.ClaimTranslations(ctx, "t2", nil, 10*time.Minute, 5)
	if err != nil || len(c) == 0 || c[0].TrackID != "tr1" {
		t.Fatalf("translation reclaim = %+v, %v", c, err)
	}
}

// Changed lyrics re-queue refused, skipped and given-up translations too.
func TestPGLyricsChangeRetranslatesSettled(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	for _, id := range []string{"refused", "skipped", "givenup"} {
		seed(t, pool, id, time.Hour)
		if _, err := st.EnqueueTranslation(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, pool, `UPDATE lyrics_translations SET status = 'refused' WHERE track_id = 'refused'`)
	exec(t, pool, `UPDATE lyrics_translations SET status = 'skipped' WHERE track_id = 'skipped'`)
	exec(t, pool, `UPDATE lyrics_translations SET status = 'error', attempts = 5, next_attempt_at = NULL WHERE track_id = 'givenup'`)
	if c, _ := st.ClaimTranslations(ctx, "t1", nil, 10*time.Minute, 5); len(c) != 0 {
		t.Fatalf("settled translations claimed: %+v", c)
	}
	for _, id := range []string{"refused", "skipped", "givenup"} {
		if ok, _ := st.RequeueLyrics(ctx, id); !ok {
			t.Fatal(id)
		}
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5); len(got) != 3 {
		t.Fatalf("lyrics claim = %v", ids(got))
	}
	for _, id := range []string{"refused", "skipped", "givenup"} {
		changed := domain.Lyrics{Status: domain.LyricsFound, Plain: "new " + id}
		if ok, re, err := st.SetLyrics(ctx, "w1", id, changed); err != nil || !ok || !re {
			t.Fatalf("%s = %v %v %v", id, ok, re, err)
		}
	}
	c, err := st.ClaimTranslations(ctx, "t1", nil, 10*time.Minute, 5)
	if err != nil || len(c) != 3 {
		t.Fatalf("translation claim = %+v, %v; want all three", c, err)
	}
	for _, x := range c {
		if x.Status != "" || x.Attempts != 0 {
			t.Errorf("%s: status %q attempts %d; a stale translation starts over", x.TrackID, x.Status, x.Attempts)
		}
	}
}

// A failed re-lookup of stored lyrics keeps them and retries with the error
// backoff; a re-lookup that finds nothing keeps them and ends.
func TestPGFailedRelookupKeepsLyrics(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	seed(t, pool, "keep", time.Hour)
	if ok, _ := st.RequeueLyrics(ctx, "keep"); !ok {
		t.Fatal("requeue")
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1); !eq(ids(got), []string{"keep"}) {
		t.Fatalf("claim = %v", ids(got))
	}
	if err := st.DeferLyrics(ctx, "w1", "keep", domain.LyricsError, time.Now().Add(time.Hour), "timeout"); err != nil {
		t.Fatal(err)
	}
	tl, _ := st.TrackLyrics(ctx, "keep", false, store.TranslationCaps{Daily: 1})
	if tl.Status != domain.LyricsFound || tl.Plain == nil || tl.NextAttemptAt == nil || tl.LastError != "timeout" ||
		tl.Attempts != 1 || tl.Queued {
		t.Fatalf("after a failed re-lookup = %+v", tl)
	}
	exec(t, pool, `UPDATE lyrics SET next_attempt_at = now() - interval '1 second' WHERE track_id = 'keep'`)
	got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 1)
	if !eq(ids(got), []string{"keep"}) || got[0].Status != domain.LyricsFound || got[0].Attempts != 1 {
		t.Fatalf("retry claim = %+v", got)
	}
	if err := st.DeferLyrics(ctx, "w1", "keep", domain.LyricsNotFound, time.Now().Add(720*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	tl, _ = st.TrackLyrics(ctx, "keep", false, store.TranslationCaps{Daily: 1})
	if tl.Status != domain.LyricsFound || tl.NextAttemptAt != nil || tl.LastError != "" || tl.Attempts != 0 {
		t.Errorf("after a not-found re-lookup = %+v", tl)
	}
	if got, _ := st.ClaimLyrics(ctx, "w1", nil, 10*time.Minute, 5); len(got) != 0 {
		t.Errorf("claimed again = %v", ids(got))
	}
}

// A track that can never be looked up (no artist) is never pending; a
// pending row reports no source.
func TestPGUnlookupableNeverPending(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `INSERT INTO tracks (id, name, duration_ms) VALUES ('noartist', 'x', 1)`)
	caps := store.TranslationCaps{Daily: 1}
	if got, err := st.NowLyrics(ctx, "noartist", caps); err != nil || got.Status != "" {
		t.Errorf("now lyrics = %q, %v", got.Status, err)
	}
	tl, err := st.TrackLyrics(ctx, "noartist", false, caps)
	if err != nil || tl.Status != domain.LyricsPending || tl.Waiting || tl.Queued {
		t.Errorf("track lyrics = %+v, %v", tl, err)
	}
	bare(t, pool, "p", time.Minute)
	exec(t, pool, `INSERT INTO lyrics (track_id, status, source) VALUES ('p', 'pending', 'lrclib')`)
	tl, _ = st.TrackLyrics(ctx, "p", false, caps)
	if tl.Source != "" || !tl.Queued || !tl.Waiting {
		t.Errorf("pending row = %+v", tl)
	}
}
