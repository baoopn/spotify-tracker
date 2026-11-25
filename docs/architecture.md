# Architecture

## Services and responsibilities

- **api** is the only component with database access. All business rules
  (history, replay detection, enrichment scheduling, token handling) live in
  `internal/tracker`, `internal/spotify.Manager` and `internal/store`.
- **poller** and **enricher** are stateless workers that talk to the API over
  gRPC (`api/proto/tracker/v1/tracker.proto`). They can be restarted or scaled
  without coordination.
- **PostgreSQL** is the source of truth. **Redis** holds derived and ephemeral
  state: caches, the enrichment stream,
  Spotify access tokens, OAuth state, heartbeats and analytics counters.

## gRPC services (internal, port 9000)

| Service | Caller | Purpose |
|---|---|---|
| `IngestService.RecordPlayback / ClearPlayback` | poller | Persist a playback snapshot in one transaction. |
| `IngestService.PrefetchUpcoming` | poller | Store the next tracks of the Spotify queue (catalog rows only, never a play) and start their lookups; see [Queue prefetch](#queue-prefetch). |
| `EnrichmentService.ListPendingTracks / SubmitTrackFeatures / ReportEnrichmentFailure` | enricher | Work queue backed by `track_features`. |
| `SpotifyService.GetAccessToken` | poller | Short-lived Spotify access tokens. The client secret and refresh token never leave the API. |
| `grpc.health.v1.Health` | probes | Standard health checking. |

Calls are authenticated with a shared bearer token (`INTERNAL_API_TOKEN`).
The port is never published: it lives on the private Docker network. Clients
retry `UNAVAILABLE` with exponential backoff.

## Data model

```
artists ─┐                 ┌─ albums
         └─ track_artists ─┤
                           tracks ──┬── listening_history   (track_id, played_at) unique
                                    ├── track_features      1:1, enrichment status + backoff
                                    └── current_playback    singleton row
listen_state (singleton: the listen in progress, for play counting)
settings (encrypted secrets) · admin_audit · api_request_stats (hourly aggregates)
tracks.track_number / disc_number · albums.album_type / total_tracks / release_date_precision
albums.upc / copyrights / details_fetched_at · artists.genres / genres_fetched_at
top_playlists ── top_playlist_tracks   (one generated playlist per completed week/month/year)
```

Migrations are plain SQL in `migrations/` (goose, embedded in the binary,
applied under a Postgres advisory lock).

The Spotify metadata columns are all nullable. The poller fills track and
disc numbers, album type, total tracks and release date precision from
`currently-playing` (and from the queue, see [Queue prefetch](#queue-prefetch)); an
empty value never overwrites a stored one. The background jobs below fill the
rest. `tracks` can hold rows that were never played (prefetched from the
queue, or skipped before they counted as a play). `artists.genres` is NULL when unknown and
`'{}'` when Spotify lists none. `albums.copyrights` is NULL until the album is
looked up.

## Playback → history rules

Each snapshot updates the *listen* in progress (`tracker.Step`): the track,
its progress, the time heard so far and whether it was counted. The listen
lives in the single-row `listen_state` table. It is read with
`SELECT ... FOR UPDATE` in the same transaction that writes
`current_playback` and the history change, and saved there too, so load,
decide and save are atomic: a database error writes nothing, and concurrent
or retried snapshots are decided one at a time.

1. **A listen counts once**, when at least `PLAY_COUNT_MIN_FRACTION` (30%,
   at most 90%) of the song has been heard, less up to 3 s for the unseen
   end of the song (so a play counts slightly before the exact fraction),
   or 30 s when the duration is unknown. The history row's
   `played_at` is when the listen started (Spotify's timestamp), not when it
   crossed the threshold. Skips below the threshold are not counted.
2. **Time heard** grows only by forward progress while playing, capped by the
   wall-clock time since the previous snapshot and, when Spotify's last
   playback change (a seek or resume) falls in that gap, by the time since
   it (plus 2 s, at most 30 s per snapshot). Seeking forward, or seeking
   while paused, adds nothing. A new listen starts with what was heard
   before its first snapshot: its progress, at most the time since Spotify's
   last playback change. Without a Spotify timestamp these timestamp caps
   are skipped. Snapshots older than the last one are ignored and never
   move `current_playback` back.
3. **Pause and resume** continue the same listen. "Nothing playing" keeps the
   listen for `PLAY_RESUME_WINDOW` (24 h), so resuming the song later is not a
   new play, and a song already counted is never counted again for it.
4. **A new listen of the same song** starts when the progress goes back to
   the start (under 10 s or 5%) from past `REPLAY_RESET_THRESHOLD` (10 s),
   jumps back by more than that on a counted listen, or loops from the last
   15% of the song to the start (repeat-one, even for very short songs).
   When it reaches the threshold it increments `replay_count` of the history
   row it repeats (`listen_state.row_played_at`; the latest row of the track
   when unknown). A listen is never applied twice (`applied_at`). A smaller
   seek back stays in the same listen.
5. **A different track**, or the same one after the resume window, starts a
   new listen (a later return to a song is a new row). An uncounted listen is
   dropped.

These rules apply from the release that introduced them (November 2026);
earlier history, counted on the first snapshot of every track, is not
rewritten. When `listen_state` is still empty, the old Redis hash
(`playback:state`) is read once as an already counted listen, so the song
playing during the upgrade is not counted twice, and then deleted. Nothing
writes that Redis key any more.

The `(track_id, played_at)` unique key keeps ingestion and imports
idempotent.

## Enrichment

When a new track is seen, a `pending` row is created in `track_features` and
the track is pushed to a Redis stream (consumer group `enricher`). The
enricher also sweeps pending rows every `ENRICH_INTERVAL_MS` as a safety net.
Lookups are rate limited client-side (`SOUNDCHARTS_RPS`, shared by
`ENRICH_CONCURRENCY` workers). A 429 pauses all workers for `Retry-After`.
Failures back off exponentially (1 min → 24 h) up to `ENRICH_MAX_ATTEMPTS`,
then the track is marked `failed`; it can be re-queued from the dashboard.
Rate limits (429, or 503 with `Retry-After`) are reported as `retryable` and
only push `next_attempt_at` back (`Retry-After` when sent, else the backoff
for the attempts so far) without using up an attempt, for at most 24 hours
from the first one (`postponed_since`); after that they count again. Other
5xx, network and body errors always count.
ISRCs unknown to Soundcharts are marked `unavailable`.

## Spotify tokens

- Authorization Code flow from the dashboard (Settings → Spotify). The
  callback is `PUBLIC_URL/admin/api/spotify/callback`, protected by a
  single-use `state` stored in Redis and tied to the admin who started it.
- The refresh token is stored AES-256-GCM encrypted (`SECRETS_KEY`) in
  `settings`. Rotated refresh tokens are persisted automatically.
- Access tokens are cached in Redis until a minute before expiry. On a 401 the
  poller asks for a forced refresh.
- Refresh tokens expire 180 days after authorization. The API emails the
  configured recipients 14/7/3/1 days before expiry, on expiry, and when
  Spotify answers `invalid_grant` (de-duplicated through Redis).

## Artist images and genres

The API fills `artists.image_url` and `artists.genres` in the background
(`internal/artistimages`) from `GET /artists/{id}`. Every minute it looks up
to 25 artists, 2 s apart, and re-checks each one every 30 days. Artists that
were never checked come first, then artists whose genres were never looked up
(`genres_fetched_at` is NULL), so every artist stored before genres existed is
looked up once more. Within each group, and among stale artists, the most
recently played come first, then the oldest lookup. A 429 pauses it for
`Retry-After`. A 5xx or network error skips that artist until the next batch;
three such errors in a row end the batch. A refused lookup or an artist with
no image keeps any image stored earlier, and a refused lookup keeps the stored
genres. Genres are
trimmed and de-duplicated, at most 20 per artist. Clients fall back to the
artist's initial when there is no image. Genres are shown only on track
detail endpoints.

## Album details

`internal/albumdetails` fills in album details from `GET /albums/{id}`:
`album_type`, `total_tracks`, `release_date_precision`, the UPC
(`external_ids.upc`), the copyrights (at most 10, each at most 500
characters) and the track and disc numbers of the album's tracks. Every
minute it looks up to 15 albums, 2 s apart: albums never checked first (most
recently played first), then the oldest lookups. Each album is re-checked
every 180 days. Album fields and track numbers are written in one statement,
and a missing value never overwrites a stored one. Only the first page of the
track list is read (up to 50 tracks); tracks past it get their numbers from
the poller. Relinked tracks are matched through `linked_from`.

A 403, 404 or other 4xx only records the lookup time (`details_fetched_at`)
and leaves `copyrights` NULL, so the album is tried again after 7 days. A 403
is logged at warn and a 404 at debug, so a refusal of every album stands out
in the logs. A 429 pauses the job for `Retry-After`. A 5xx, a network or
decode error, or a 401 that persists after one token refresh counts as
transient: the album is retried in about an hour (its lookup time is set to
`now - 180 days + 1 hour`) and the batch goes on with the next album; three
transient errors in a row end the batch. Nothing is looked up while Spotify
is not connected.

## Lyrics

`internal/lyrics` looks up track lyrics on [LRCLIB](https://lrclib.net)
(`internal/lrclib`, no API key) and stores them in the `lyrics` table
(migration `00014_lyrics.sql`; the queue columns are `00017_lyrics_queue.sql`,
manual lyrics `00019_manual_lyrics.sql`).
Lyrics are admin and MCP only, with one
public exception: the [lyrics of the song playing now](#public-now-playing-lyrics).
No other `/api/v1` route, no public MCP tool and none of the shared track JSON
types carry them (guard tests in `internal/httpapi` and `internal/mcpserver`).

- **Queue.** The `lyrics` table is the lookup queue, like
  `lyrics_translations`. Every `LYRICS_INTERVAL` (1 min) a worker claims a
  batch of `LYRICS_BATCH` (10) tracks with `store.ClaimLyrics`: it first
  inserts a `pending` row for the tracks with no row that come next in line
  (at most one batch, tracks with a title and an artist), then leases rows
  with `SELECT ... FOR UPDATE SKIP LOCKED` (`claimed_by` / `claimed_until`,
  `LYRICS_LEASE`, 15 min, at least one batch at 3 requests per track, each
  with the spacing and the 20 s request timeout:
  3 × `LYRICS_BATCH` × (`LYRICS_SPACING` + 20 s), 11 min for the defaults). So API replicas never
  look up the same track, and a crashed worker's claim expires and is taken
  again. The pending rows are inserted in id order, so replicas inserting the
  same tracks cannot deadlock. A row is claimable when no live lease holds it
  and it is `pending`, re-queued (`requested_at`, or `claim_requested_at`
  left by a worker that crashed: the re-queue survives), or its retry is due
  (`next_attempt_at`).
- **Order.** The song playing now, then the upcoming songs from
  [queue prefetch](#queue-prefetch) (`array_position` over the priority
  list), then admin re-queues (newest first), then by last play (most recent
  first), then the rest of the library (never played; new tracks before
  retries on a tie). The whole library is backfilled at the lowest priority
  (LRCLIB is free). A new song playing (`Wake`) or an admin re-queue
  (`Nudge`) stops a running batch: its other claims are released and the
  next batch claims again at once. A new upcoming list waits for the batch to
  end. During a pause (rate limit, bad batches) the refresher waits at least
  the interval and no wake cuts it short.
- **Claims.** Results (`SetLyrics`) and deferrals (`DeferLyrics`) are written
  only while the worker holds the claim; a result after a lost lease is
  dropped. A rate limit, a stop after three transient errors, a new song and
  shutdown release the unprocessed claims without an outcome
  (`ReleaseLyrics`); a re-queue mark taken by the claim
  (`claim_requested_at`) is put back, so the track stays first in line. A
  re-queue while a worker holds the track keeps the lease, and the track is
  looked up again after that worker finishes.
- **Client.** The client sends at most one request per `LYRICS_SPACING`
  (2 s) and identifies itself with the User-Agent
  `spotify-tracker (https://github.com/baoopn/spotify-tracker)`.
  `LYRICS_ENABLED=false` makes no LRCLIB calls (nothing is claimed).
- **Matching.** LRCLIB often has several entries for one song, for example
  one crediting only the first artist with plain lyrics and one crediting
  every artist with synced lyrics. With an album and a duration (rounded
  seconds) the lookup tries, in order: `GET /api/get` with all credited
  artists joined by ", " (as LRCLIB writes them), `GET /api/get` with the
  first artist only (skipped for a single artist), then `GET /api/search`
  (track name and first artist). It stops at the first result with synced
  lyrics. An exact match with only plain lyrics, or `instrumental`, is kept
  while the search runs; a search candidate replaces it only when it has
  synced lyrics, the same folded track name, a duration within 2 s and a
  credit naming the first artist (a cover by someone else or the featured
  artist alone does not count). So an instrumental exact match flips only to
  synced lyrics, never to plain. A failed Get does not end the lookup; when
  nothing is found its error is returned, so the track is retried instead of
  marked not found. When the search fails after an exact match was found
  (also on the 20 s request timeout), the exact match is stored; a rate
  limit or shutdown still retries the whole lookup. Without an exact match,
  a search candidate must have the same folded track name (a version suffix
  such as "(Live)" or "- Remastered" is ignored), an artist matching one of
  the credited artists, and, when the duration is known, a duration within
  2 s. Among those it prefers the exact name, then synced lyrics, then plain
  lyrics, then the closest duration. Artist credits are split into names on
  ",", "&", ";", "feat.", "ft.", "featuring" and "x" and folded; two names
  match when equal, or when the shorter (at least 3 letters) is whole words
  of the longer, so short names like "V" or "Ni" match only themselves.
  Text that is only markers ("[Instrumental]", "♪") counts as no lyrics. A
  lookup sends at most 3 requests, and 3 only when the earlier ones found no
  synced lyrics. The chosen entry's id is stored as `lrclib_id`. Songs stored
  with plain lyrics before this matching are not looked up again on their
  own: they change only when re-queued (admin "Look up again", MCP
  `admin_requeue_lyrics`) or on a normal not-found retry. Synced lyrics
  change the lyrics hash, so an existing translation is re-queued.
  Without an album or duration only search is used. NUL bytes are removed,
  line breaks normalized to `\n`, and each text is capped at 200 000 bytes.
  When only synced lyrics exist, plain text is derived by removing the
  timestamps; a record with no lyrics text left is `instrumental`.
- **States.** `pending` waits for its first lookup (`fetched_at` null).
  `found` and `instrumental` are final (never overwritten by a later failure
  or not-found; only a successful re-lookup replaces them). A failed
  re-lookup keeps them and is retried with the error backoff
  (`next_attempt_at`, `attempts` counts failed re-lookups); a re-lookup that
  finds nothing keeps them and ends. `last_error` is always the last
  lookup's. `not_found` is retried after `LYRICS_NOT_FOUND_RETRY`
  (30 days). A transient error (5xx, 408, timeout, network, decode) or a
  failed database write is retried after 1 h, doubling per error in a row up
  to 7 days (`attempts` restarts at 1 when the status changes, so monthly
  not-found checks do not grow the error backoff). Three transient errors in
  a row end the batch, and the next batch waits twice the interval, doubling
  per such batch up to 1 h; a batch with a successful lookup resets it. A
  permanent 4xx is retried after 7 days and does not end the batch. A 429
  writes nothing and pauses the job for `Retry-After` (seconds or an HTTP
  date; default 1 min, at most 1 h). A track without a row is reported as
  `pending`.
- **Re-queue.** `POST /admin/api/tracks/{id}/lyrics/requeue` (MCP
  `admin_requeue_lyrics`) puts a track at the front whatever its status:
  not looked up, `not_found`, `error`, or `found` (stale lyrics are fetched
  again). Stored lyrics stay served until the new lookup, and the error
  backoff restarts. Manual lyrics answer `409 manual_lyrics` unless the
  request sets `replace_manual: true` (see Manual lyrics below). While `LYRICS_ENABLED` is false the re-queue answers
  `409 lyrics_disabled`, and the admin hides "Look up again" and shows
  nothing as queued (`lookup_enabled` in `TrackLyrics`). `GET /admin/api/lyrics/queue` (MCP `admin_lyrics_queue`)
  counts the queue (queued, re-queued, never looked up (no row or a pending
  row), in progress, found,
  instrumental, not found, failed), the next retry and the last lookup. The
  admin track page and the overview's now-playing lyrics show the state
  (Queued, Looking up…, Not found / next try, Lookup failed) and a "Look up
  again" button; the overview has a "Lyrics queue" card.
- **Changed lyrics.** When a lookup stores lyrics that differ from what was
  stored (the source hash over synced and plain text) and from what was
  translated, and the track has a translation row, `SetLyrics` sets that
  row's `requested_at` in the same statement: the translation goes to the
  front of the translation queue, and the fetcher's `OnFound` hook wakes
  the translator. It only sets the mark, so a live translation lease is
  kept; that worker's result no longer matches the lyrics and is not stored.
  Unchanged lyrics touch nothing (no Claude call). No translation row is
  created for a track that was never translated (no backfill; the song
  playing now and the next one are queued by the translator's own paths).
  Lyrics that become `instrumental` are not translated, and their old
  translation is no longer served or claimed (it joins `found` lyrics only).
  A re-lookup that finds nothing keeps the stored lyrics. This holds for an
  admin or MCP re-queue and for any other lookup.
- **Manual lyrics.** The admin can add lyrics by hand (`source = 'manual'`,
  migration `00019_manual_lyrics.sql`, with `manual_at` / `manual_by` and
  `replace_manual`): `PUT /admin/api/tracks/{id}/lyrics` (MCP
  `admin_set_lyrics`) takes `{ synced?, plain?, instrumental? }`. The texts
  are normalized (BOM, NUL, CRLF, trailing spaces) and capped like LRCLIB
  text; synced text is checked line by line by `internal/lrc.Validate`
  (valid time tags, single-tag lines in time order, nothing more than 5 s
  past the track's end, 1 to 2000 timed lines) and a bad line is a
  `400 invalid_lrc` naming it; with only synced text the plain text is
  derived (`lrc.Plain`: tags removed, word-less lines made stanza breaks).
  `store.SetManualLyrics` uses the hash rule of `SetLyrics`: changed lyrics
  put an existing translation first in line, unchanged ones touch nothing,
  and no translation row is created. Manual lyrics win: `lyricsHeldSQL`
  (manual, no `replace_manual`) keeps the row out of every automatic claim
  (backfill, retries, now-playing and prefetch priority) and out of the
  queued / waiting states. A manual save clears the re-queue marks and the
  retry state but never breaks a live lease; `SetLyrics` locks the row
  (`FOR UPDATE`) and, on held manual lyrics, only gives the lease back, and
  `DeferLyrics` leaves them as they are, so a lookup that finishes later
  cannot overwrite them. An explicit re-queue with `replace_manual: true`
  lets the next LRCLIB result replace them (`source` becomes `lrclib`); a
  lookup that finds nothing ends the request and keeps them, a failed one is
  retried. `DELETE /admin/api/tracks/{id}/lyrics/manual` (MCP
  `admin_clear_manual_lyrics`) removes them and puts an LRCLIB lookup first
  (a track with no title or artist loses its row). The admin track page has
  an editor (Synced (LRC) and Plain tabs, live preview with per-line errors
  from the same rules in `src/lib/lrc.ts`, Instrumental, a local
  "Import .lrc file") and shows the source as "Added by you". Saving LRCLIB
  lyrics unedited keeps their hash: the editor prefills and sends LRCLIB's
  plain text, and the API keeps the stored bytes when the new text differs
  only by normalization, so no translation is redone.
- **Search.** `search_tsv` is a stored generated column,
  `to_tsvector('simple', search_fold(plain_lyrics))`, with a GIN index. The
  `simple` configuration fits Vietnamese (space-separated syllables, no
  stemming or stop words) and `search_fold` (migration 00010) folds accents
  without an extension. Queries use `websearch_to_tsquery`; exact phrases
  (`phraseto_tsquery`) rank first. A search is cut off after 5 s. A later change to `search_fold` must
  recompute the column (`UPDATE lyrics SET plain_lyrics = plain_lyrics`).

### Public now-playing lyrics

`GET /api/v1/now-playing/lyrics` (`internal/httpapi/now_lyrics.go`) serves the
public page the full lyrics of the song playing now, and of no other song:

- **Current track only.** It takes no parameters (a query string is ignored)
  and reads the now-playing snapshot. It answers `{"data":null}` when there is
  no current track, the snapshot is older than 60 s, the estimated position
  is past the track's end, or no lyrics were found.
- **Paused.** A paused track keeps its lyrics. While Spotify still reports a
  current item, paused or not, the poller rewrites the snapshot and its
  `fetched_at` on every poll, so a paused snapshot stays fresh. When Spotify
  reports nothing, the poller clears playback: the snapshot keeps the last
  track with `is_playing: false`, but `fetched_at` stays at the time of the
  clear, so the 60 s rule ends the lyrics for an idle player as it does for a
  poller that stopped. While paused the position does not advance (the
  snapshot's `progress_ms`). The page keeps the lyrics card in both layouts
  with the highlight frozen on the paused line, and does not poll while
  paused unless the lyrics or translation are pending (no 30 s re-check of a
  null answer either). Resuming reuses the same cached answer.
- **Status.** The body has `status`: `found`, `instrumental`, or `pending`
  (no text). Pending follows the lookup queue and always ends: no lookup yet
  (no row, or `pending`) while the lyrics fetcher runs (`LYRICS_ENABLED`), a
  re-queued or leased track without stored lyrics, or a not-found or failed
  lookup retried within two minutes; otherwise the answer is null. A track
  with no title or artist is never looked up, so it is never pending. Found or
  instrumental lyrics are served while a fresh lookup is queued. The page shows "Looking for
  lyrics…" and asks again every 10 s while lyrics or their translation are
  pending, every 30 s while the answer is null, and stops once both are
  settled. The lyrics fetcher is woken for the song playing now, so pending
  clears quickly.
- **Full lyrics.** `store.NowLyrics` reads the track's found or instrumental
  lyrics. Synced LRC text is parsed by `internal/lrc` (several time tags per
  line, `[offset:]`, metadata and untimed lines dropped) and sent as
  `lines: [{ start_ms, text }]` with `synced: true` (at most 2000 lines of at
  most 200 runes each, and 100 000 runes in all; later lines are dropped).
  Without synced lines the trimmed plain text is sent as `plain` (at most
  20 000 runes). Instrumental tracks get
  `instrumental: true` and no text. The page highlights the current line
  from its own position estimate.
- **Translation.** With `TRANSLATE_ENABLED`, found lyrics carry
  `translation: { status: 'pending' | 'done', source_language, languages, en, vi, credit }`
  (see [Lyrics translation](#lyrics-translation)). Each language is
  `{ lines, plain }`: for synced lyrics `lines` has one entry per served line
  (`""` for empty lines; a language whose line count does not match the
  parsed LRC is dropped), for plain lyrics `plain` holds the text. A null
  language means the song is all in that language; `languages` lists every
  language in the lyrics, main first (null for older translations). `translation` is null when the
  feature is off, for instrumental or pending lyrics, and when there will be
  no translation (skipped, refused, given up). The translation is `pending`
  only while it will be worked on soon: leased now, or queued, stale or a
  retry due within two minutes while no cap is reached. A retry further
  away, or one held back by a reached cap, shows no translation, so the
  "Translating…" tag and the 10 s poll cannot run for a whole song. Credit:
  "Translated by Claude".
- **Credit and caching.** Every answer carries `credit`: `{ text: "Lyrics
  from LRCLIB", url: "https://lrclib.net" }`, or for lyrics added by hand
  `{ text: "Lyrics added by Bao", url: null }` (the page shows it as plain
  text, the site's name for its owner). Manual lyrics are otherwise served
  the same way (synced lines or plain text). `Cache-Control: no-store`, so Cloudflare
  never caches it. The current track's answer is kept in memory for 1 min
  (10 s while there are no lyrics, or the lyrics or translation are pending),
  and concurrent misses share one store read. So each replica reads the
  database about once a minute while one track plays (every 10 s while
  something is pending), plus once per track change.
- **Not in MCP.** The route is registered next to `v1Routes`, not in it, so
  `V1Handler` (and with it the MCP server) cannot reach it.

### Lyrics translation

`internal/translate` translates found lyrics into English and Vietnamese with
the Claude API (Go SDK `anthropic-sdk-go/v2`), off unless `TRANSLATE_ENABLED`
and `ANTHROPIC_API_KEY` are set. Results are stored in `lyrics_translations`
(migration `00015_lyrics_translations.sql`) and shown on the public
now-playing lyrics and in the admin.

- **Queue, no backfill.** A song is queued only when it plays: the tracker
  wakes the worker with the song playing now (`Wake`), and each batch also
  queues songs played within `TRANSLATE_RECENT_WINDOW` (30 min) that have
  lyrics (this covers lyrics found after the song started, and plays another
  replica saw). An admin can re-queue any track. Old songs that have not
  played since the feature shipped are never queued on their own.
- **Upcoming songs.** With queue prefetch on and `QUEUE_PREFETCH_TRANSLATE`
  (default true), the next song of the Spotify queue (position 0 only) is
  queued too, so its translation is ready when it starts. This is the one
  exception to "queued when it plays": a song that is then skipped still
  costs a call, within the caps.
- **Order.** The song playing now first, then the upcoming song, then
  re-queued songs (newest request first), then by last play. The song
  playing now counts as queued only once its row exists (it may have no
  lyrics yet); when LRCLIB finds its lyrics, the fetcher tells the
  translator, which queues it then (also with `TRANSLATE_RECENT_WINDOW=0`),
  so a new song gets lyrics and then a translation within about a minute.
- **Leases.** The worker claims one song at a time with
  `UPDATE ... FOR UPDATE SKIP LOCKED` and a lease (`claimed_by`,
  `claimed_until`, `TRANSLATE_LEASE`, 10 min; a re-queue taken by a claim
  whose worker crashed stays re-queued through `claim_requested_at`). Results are written only while
  the claim is held and the lyrics still match the hash they were read with
  (`source_hash`); a crashed worker's lease expires and the song is claimed
  again. So API replicas never translate the same song twice. A re-queue
  never clears a live lease: the song is queued again when the current
  worker finishes.
- **Calls and caps.** One Claude call at a time per process, at least
  `TRANSLATE_SPACING` (15 s) apart (no wait after a song that needed no
  call). Before each call one call is counted in `translation_usage` (per UTC
  day) if `TRANSLATE_DAILY_CAP` and `TRANSLATE_MONTHLY_CAP` allow it; the
  check and the increment run in one transaction under
  `pg_advisory_xact_lock`, so replicas cannot both take the last call. At a
  cap the worker pauses until the next UTC day (at most 6 h) and checks
  again. The SDK does not retry; the worker does, so every call counts once.
  Billed tokens are recorded for every answer, also unusable ones.
- **Request.** Model `TRANSLATE_MODEL` (`claude-sonnet-5-5`, $2 / $10 per
  MTok), effort `TRANSLATE_EFFORT` (low; set explicitly, the API default is
  high), thinking left unset (adaptive; `disabled` is a 400 on this model), structured JSON
  output (`{ languages, source_language, en, vi }`), the fixed system prompt first with a
  cache breakpoint, the server-side refusal fallback (`fallbacks: "default"`).
  Synced lyrics go as numbered non-empty lines and come back as one line per
  input line; the worker puts empty lines back, so `en_lines` / `vi_lines`
  line up with `internal/lrc.Parse`. Plain lyrics go as one text.
  Mixed-language songs (K-pop, J-pop, V-pop, rap) are the norm: the prompt
  translates into each target on its own, line by line and part by part, so
  English lines get Vietnamese and Vietnamese lines get English; only words
  already in the target, proper nouns, numbers, common loanwords ("VIP") and
  pure vocalisations are copied. `languages` lists every language found,
  main first (`source_language`, stored in `lyrics_translations.languages`).
  A language counts only when a full phrase or line is in it (loanwords,
  names and ad-libs do not). A target should be null only when `languages`
  is just that language; a null target in a mixed song is still stored as
  none (no retry, it costs credit) and logged as a warning with the track
  id. Structurally bad answers (line count, codes) are `error`s, retried. Before the lyrics, the user
  message holds a `<song>` block read by `TranslationContext` (a separate
  read, not part of the claim): title, all credited artists, album, album
  type, release year, artist genres, enrichment genres, explicit flag
  and length; unknown fields are left
  out, and a failed read sends the song without them. The system prompt stays
  fixed, so it still caches. `<lyrics>` and `<song>` tags in any field or the
  text are neutralized, so song text and metadata cannot leave their block.
  Existing translations are not redone for a new model or prompt (there is
  no backfill, and calls cost credit): re-queue a song in the admin to
  translate it again with the current model, prompt and context (also for
  mixed-language songs translated before the per-language prompt). Songs
  over 250 lines or 6 000 characters are `skipped` without a call (both
  translations must fit one answer).
- **Outcomes.** `done`; `refused` (terminal); `skipped` (also an answer cut
  off at `max_tokens`: "too long", not retried); `error` retried with
  backoff from 30 min (doubling, at most 7 days) and given up after
  `TRANSLATE_MAX_ATTEMPTS` (5) failures in a row (`next_attempt_at` null).
  A rate limit pauses for its Retry-After; a bad key or a spent credit pauses
  for 6 h and logs an error (never the key). Stored errors never hold the
  response body. When the lyrics change, the row is stale and translated again.
- **Admin and MCP.** `GET /admin/api/lyrics/translation-queue` (MCP
  `admin_translation_queue`) counts the queue and this month's calls;
  `POST /admin/api/tracks/{id}/translation/requeue` (MCP
  `admin_requeue_translation`) puts a track first.

## Queue prefetch

While a song plays, the poller reads the user's Spotify queue
(`GET /me/player/queue`, scope `user-read-playback-state`) and the API
prepares the next tracks before they start: catalog rows, lyrics,
translation, album and artist details, and the public lyrics cache. On by
default (`QUEUE_PREFETCH_ENABLED`).

- **Cadence (poller).** The queue is read only while something plays: when
  the song changes, and every `QUEUE_PREFETCH_RECHECK` (60 s) while it plays
  on, to catch queue edits. The read runs in its own goroutine with a 10 s
  timeout (and the RPC to the API its own 10 s), so a slow queue never delays
  the 3 s poll. A 429 on the queue read pauses only queue reads, for
  Retry-After (at least one recheck interval). A 429 on the main poll pauses
  queue reads too, since Spotify limits are app wide; a new pause never
  shortens one already running. A 403 backs off for an hour. The poller's
  token cache is never locked across the token RPC: concurrent refreshes
  share one call, and the poll keeps using the still-valid token meanwhile.
- **What is sent.** The next `QUEUE_PREFETCH_DEPTH` (1) tracks; episodes,
  local files, invalid ids and repeats are skipped, and so is the song playing
  now (repeat-one). A track prefetched within `QUEUE_PREFETCH_TTL` (6 h) is
  sent as listed only; a track is marked done only after the API stored it.
  An empty queue is still sent, so the up-next list clears. The API also
  drops repeats and the song playing now. When playback stopped or moved to
  another song while the queue was read, it still stores the rows, but it
  does not save the list or wake any lookup.
- **Catalog only.** `IngestService.PrefetchUpcoming` upserts artists, album
  and track (an empty value never overwrites a stored one). It never writes a
  play: no history row, replay, `listen_state`, `current_playback` or
  response-cache change. When a `RecordPlayback` upserting the same rows
  makes Postgres abort the transaction (deadlock or serialization failure),
  it is retried once.
- **Enrichment waits for the play.** Prefetch does not set the catalog-seen
  marker and creates no `track_features` row, so the real play still creates
  the row and queues Soundcharts enrichment. `QUEUE_PREFETCH_ENRICH=true`
  enriches upcoming tracks at once instead (uses quota).
- **Lookups.** The lyrics fetcher and the translator take a priority list:
  the song playing now, then the upcoming songs, before everything else.
  Lyrics lookups use the whole upcoming list; translation gets only the very
  next song (position 0), whatever `QUEUE_PREFETCH_DEPTH` is, when
  `QUEUE_PREFETCH_TRANSLATE` is on (see [Lyrics translation](#lyrics-translation)).
  An upcoming list expires after 15 minutes without a refresh, is cleared
  when playback goes idle or the queue empties, and a song leaves it once it
  plays, so stale ids never outrank admin re-queues. With
  `QUEUE_PREFETCH_DETAILS` the album-details and artist-image jobs wake, and
  their never-checked-first order picks the new album and artists.
- **Cache.** The public now-playing lyrics memo holds several tracks (8).
  Upcoming tracks whose lyrics are stored (or just found) are built into it
  ahead of time with a 10-minute TTL, so the first request after the switch
  is served from memory; from that first request on, the normal one-minute
  TTL applies. Warming runs in the same flight as requests, so it never
  overwrites a newer body, and a translation finished for an upcoming song
  rebuilds its entry. Bodies still pending are not warmed. Only the replica
  that received the RPC is warmed and woken; other replicas pick the tracks
  up in their next batch.
- **Up next (private).** The list is kept in Redis (`playback:upnext`,
  5 min, cleared when nothing plays) and shown in the admin overview
  (`up_next`) and the `admin_overview` MCP tool. It is never on the public
  site or the public `now_playing` tool.

## Top 50 playlists

`internal/topplaylists` runs in the API (first tick a minute after start,
then every 10 minutes). For each enabled kind (`TOP_PLAYLISTS_PERIODS`) it
handles only the most recently completed period — ISO week, calendar month
or year, half-open `[start, end)` at local midnights in
`TOP_PLAYLISTS_TIME_ZONE` (DST-safe). Tracks are ranked by
`SUM(replay_count)` in that range (ties: most recently played). The row in
`top_playlists` is the state machine: `pending` → `created` (or `empty` when
nothing was played); `waiting_auth` while the stored scopes lack
`playlist-modify-public` (or Spotify answers 403); `failed` retries with
backoff (5 min doubling to 6 h, or Spotify's `Retry-After`). The Spotify
playlist ID is stored right after `POST /me/playlists`, so a retry only
re-runs the idempotent `PUT /playlists/{id}/items` and never creates a
duplicate. The ranked snapshot is kept in `top_playlist_tracks`. Invalid
settings are logged and disable the feature without stopping the API.

Kinds in `TOP_PLAYLISTS_LIVE` (default `week`) also get a **live** playlist of
the running period, handled by the same tick under the same advisory lock. On
the period's first plays the playlist is created with a "so far" description
(row `created`, `live = true`), ranking `[start, now)`. Every
`TOP_PLAYLISTS_LIVE_REFRESH` (default 1 h, min 10 min; `refreshed_at` is the
last success) it is re-ranked: an unchanged order writes nothing to Spotify
and only updates the snapshot's plays; a new order replaces the items first and
saves the snapshot after Spotify accepted them. Failures never unpublish: the
row keeps `created` / `live` / `published_at` and its old snapshot, and only
`last_error` and `next_attempt_at` (one interval later) change. A failed
initial creation follows the normal `failed` / `waiting_auth` retries.

Each tick first finalizes ended live rows in place (a store query for
`created AND live AND period_end <= now - 15 min`, not backing off, oldest
first, three per tick, so rows a long outage left behind are caught up too):
full-period ranking, items replaced if the order changed, `PUT /playlists/{id}`
with the final description, then `live = false`. Failures back off
(`attempts` / `next_attempt_at`) while the row stays live and published. The
15-minute grace lets late plays count; the admin's Regenerate finalizes
immediately. The next period's live playlist starts at its own start
regardless. The APIs report `live` only while the period is running. When a
final publish reuses an existing playlist (possibly created live), its
description is updated too; if that update fails, the row is published anyway
and left live (backing off) for the finalizer to retry. If saving a new
playlist ID fails after Spotify created it, the runner keeps the ID in memory
and the next attempt saves and reuses it instead of creating another. When
the create response itself was lost (or the process died mid-create), the
next attempt on a row that already existed without a stored ID first looks
for an owned playlist with the exact name in `GET /me/playlists` and adopts
it; a final attempt then sets the final description on it.

After the generate and live steps each tick prunes, renames and checks, in
that order (a Spotify rate limit at any step ends the tick):

- **Prune.** Only the newest `TOP_PLAYLISTS_KEEP` (default 3; `0` keeps all)
  `created` rows per kind are kept, the live and hidden ones included. Older
  rows are pruned oldest first, ten per tick, and so are `failed` and
  `waiting_auth` rows that hold a Spotify playlist and have `TOP_PLAYLISTS_KEEP`
  newer `created` rows of their kind (they are no longer generated): the row becomes `pruned`
  (`pruned_at`, not live, snapshot deleted) before Spotify is asked to delete
  the playlist, so nothing restores it, and upserts never touch a pruned row.
  The removal (`DELETE /me/library?uris=spotify:playlist:{id}`, falling back
  to the deprecated `DELETE /playlists/{id}/followers` on 403 or 404; only
  that endpoint's 404 counts as already gone) sets `spotify_removed`; failures keep the row pruned with `last_error` and are
  retried each tick. Admin Generate refuses a period that would be pruned
  right away; regenerate and hide refuse pruned rows.
- **Rename.** Names are `{owner}'s Top 50 · {label}`. The owner is
  `TOP_PLAYLISTS_OWNER`, or else the connected account's display name
  (`GET /me`, cached 24 h, not read while Spotify is not connected; an expired
  name is kept when the read fails). With
  no owner at all new playlists get `Top 50 · {label}` and the rename step is
  skipped, so names never flap on a transient failure. Created rows whose
  stored name differs get `PUT /playlists/{id}` (five renamed per tick, at most
  ten Spotify writes, so failing rows cannot starve the rest). A re-followed
  playlist is renamed right after its restore.
- **Check and restore.** Every `TOP_PLAYLISTS_VERIFY` (default 1 h, min 10 min,
  `0`/`off` disables; the newest `verified_at` is the last check) the created
  rows are compared with the user's library (`GET /me/playlists`, every
  page). Rows published or restored in the last 15 minutes are skipped (a new
  playlist may not be listed yet). Nothing is changed when the list fails, is
  too long to read in full (over 1000 playlists), is empty while playlists
  are published, or contains none of the published playlists (likely
  another account; this holds for a single playlist too, so a sole published
  playlist deleted by hand is not restored). A missing
  playlist (three per check) is followed again (`PUT /me/library`, falling
  back to the deprecated `PUT /playlists/{id}/followers`; the library endpoint
  has no visibility option, so `PUT /playlists/{id} {"public": true}` follows
  it) and its items reset
  to the stored snapshot; when Spotify no longer has it at all, a new playlist
  is created and its ID saved at once (same no-duplicate rule as above). The
  row stays `created` with `restored_at`; a failure only sets `last_error`. A
  404 from a live refresh, finalize, regenerate or rename runs the same
  restore; a failed restore there backs off like other live refresh or
  finalize failures. The admin's Check now forces a check.

## Admin dashboard

SvelteKit SPA built to static files and embedded with `go:embed`, served at
`/admin` with a strict CSP. Script hashes are computed from `index.html` at
startup, and the Clerk Frontend API host is derived from the publishable key.
Authentication is by Clerk: the SPA sends the Clerk session JWT as a Bearer
token, and the API verifies it against Clerk's JWKS (authorized party = our
origin) and checks an allow-list of emails / user IDs. Every sensitive action
is written to `admin_audit`.

## MCP server

`cmd/api` also serves a remote [MCP](https://modelcontextprotocol.io) server at
`/mcp` (`internal/mcpserver`, official Go SDK), so AI clients such as Claude can
read the listening data. It uses the Streamable HTTP transport in **stateless**
mode with plain JSON responses: no server-side sessions and no SSE streams, so
it works behind Cloudflare and on several replicas. Only `POST /mcp` is used
(GET and DELETE answer 405). `MCP_ENABLED=false` removes the route (404).

- **Auth.** No `Authorization` header (or `Bearer` with an empty token) gets the
  public tools. A valid admin token adds the admin tools. Two tokens are valid:
  the **dashboard token** (generated under Settings → MCP; only its hex SHA-256
  is stored in the `mcp` setting, and the hashes are compared in constant
  time) and the optional **env token** `MCP_ADMIN_TOKEN` (bootstrap/fallback,
  at least 32 characters, constant-time compare). Any other token is rejected
  with 401 and `WWW-Authenticate: Bearer`; a wrong token is never downgraded to
  anonymous. Failed attempts are logged at warn (no token).
- **Dashboard token flow.** `POST /admin/api/mcp/token` creates (or rotates)
  a token `stm_` + 32 random bytes (base64url) and returns it once; the page
  shows it once with copy buttons and ready-made `claude mcp add` / `.mcp.json`
  snippets. Rotating or revoking (`DELETE /admin/api/mcp/token`) makes the old
  token fail with 401. The **Admin tools** toggle (`PUT /admin/api/mcp`) turns
  the admin tools off: a valid token then gets the public tools, a wrong one is
  still 401 (one exception for the env token, see below). `mcpserver` reads
  the setting through a 30 s cache that the admin API drops on every save, so
  changes apply at once on this replica and within 30 s on others. Concurrent
  requests share one load, and after a failed load the cache waits 5 s before
  it asks Postgres again. If a reload fails after the TTL ran out, the last
  loaded value is used. If there is no usable value (cold start with Postgres
  down, or a failed reload right after a save or revoke, so a revoked token
  never keeps working), the settings count as unavailable: the dashboard token
  and any other non-env token get 503, and the env token still works with the
  built-in defaults, which means **admin tools on even if the dashboard toggle
  is off**. Leave `MCP_ADMIN_TOKEN` empty if the toggle must hold during a
  database outage. The token's last use is written at most once per 5
  minutes, in the background.
  The settings and token routes are not in `admin.ReadOnlyHandler`, so an MCP
  client can never mint its own token.
- **Limits.** Own per-IP rate limit (`MCP_RATE_LIMIT_PER_MINUTE`, default 60),
  1 MiB request body, 30 s timeout. `/mcp` is not behind the `/api/v1`
  middleware, so MCP calls are not counted in the API analytics. The global
  CORS middleware still runs on `/mcp`, but it allows only `GET`, `HEAD` and
  `OPTIONS` with the `Accept` and `Content-Type` headers. A browser's
  preflight for `POST /mcp` (or for an `Authorization` header) is therefore
  refused, so MCP clients that run in a browser cannot use the endpoint.
  Desktop, CLI and server-side clients don't use CORS and are not affected.
- **Dispatch.** Each tool builds a `GET` request and runs it in-process through
  the existing handlers, so there is no second copy of the queries. Public
  tools go through `httpapi.Handler.V1Handler` (the `/api/v1` routes at the same
  paths, sharing the Redis cache, without rate limit, analytics, SSE stream or
  `POST /events`); admin tools through `admin.ReadOnlyHandler` (GET routes only,
  no Clerk middleware; never mounted publicly). Arguments are declared in each
  tool's input schema (enums, bounds, defaults) and still validated by the API.
  A 2xx answer is returned as JSON text plus structured content; a 4xx as a
  tool error with the API's message; anything else as "internal error". Path
  arguments are escaped as one segment, so they can't leave the route.
- **Public tools** (`readOnlyHint`, `openWorldHint: false`): `now_playing`,
  `recent_plays` (limit, cursor), `track_details` (id), `listening_summary`
  (range), `top_tracks` / `top_artists` / `top_genres` (range, limit),
  `audio_profile` (range), `recent_playlists` (limit), `top_playlists` (limit).
  None returns lyrics; the public now-playing lyrics are not reachable from MCP.
- **Admin tools** (read-only unless noted): `admin_overview`, `admin_spotify_status`,
  `admin_history` (limit, cursor, q), `admin_tracks` (q, status, sort, order,
  limit, offset), `admin_track` (id), `admin_track_lyrics` (id, synced),
  `admin_search_lyrics` (q, limit), `admin_translation_queue`,
  `admin_requeue_translation` (id), `admin_lyrics_queue`, `admin_requeue_lyrics`
  (id, replace_manual), `admin_set_lyrics` (id, synced, plain, instrumental)
  and `admin_clear_manual_lyrics` (id). These four are the only write tools.
  `admin_requeue_translation` only reorders the translation queue.
  `admin_requeue_lyrics` reorders the lookup queue, and with
  `replace_manual` lets LRCLIB replace lyrics added by hand.
  `admin_set_lyrics` replaces the stored lyrics, and
  `admin_clear_manual_lyrics` deletes lyrics added by hand. The last three are
  marked destructive (the MCP hint is per tool, not per argument, so the
  lyrics re-queue is marked for its replace_manual case). All four are marked
  not read-only, send their non-path arguments as a JSON body (strings
  untrimmed; the schema's `maxLength` counts characters, the API caps each
  lyrics text at 200 000 bytes and answers `too_large` above it), and are audited as `translation.requeued` / `lyrics.requeued` /
  `lyrics.manual_set` / `lyrics.manual_cleared` by the MCP actor; LRC line
  errors are part of the tool error text,
  `admin_artists` (q, sort, order, limit,
  offset), `admin_playlists` (limit, offset), `admin_top_playlists` (period,
  limit, offset), `admin_top_playlist` (period, start), `admin_backups`,
  `admin_api_analytics` / `admin_page_analytics` (range, tz). Write routes, the
  SMTP settings, the MCP settings, `/auth/me`, `/audit` and `/config` are not
  exposed (except the two re-queues and the manual lyrics routes). `mcpserver.PublicTools()` / `AdminTools()` list the names and
  descriptions for the dashboard.

## Brand assets

Logos, icons, favicons, og:image and the email logo come from the baoopn brand
API (`BRAND_API_URL`, default `https://baoopn.com/api/v1/brand/listening`), so a
file replaced in the baoopn admin shows up here within minutes, without a
deploy. `internal/brandkit` fetches the JSON every `BRAND_REFRESH` (default 60
s, 3 s timeout) and keeps the last good copy in memory; a failed refresh is
logged and never clears it. An entry is used only when its URL is https on
`media.baoopn.com` or `baoopn.com` (a relative `/media/...` URL is resolved
against the API origin) and its content type fits the slot (svg, png or ico).

- `GET /brand/{slot}` redirects (302) to the cached media URL, or to the static
  copy in `web/public/static` when nothing is cached. The frontends use these
  stable paths, so the prerendered HTML and its CSP hashes never change.
- `/favicon.ico`, `/favicon.svg`, `/apple-touch-icon.png`, `/favicon-{16,32,48}.png`,
  `/icon-{192,512}.png`, `/icon-maskable-512.png` and `/og-image.png` redirect
  to the media URL when cached, else serve the embedded static file.
- `/site.webmanifest` and `/admin/site.webmanifest` are generated; their icons
  point at the media URLs (static paths as the fallback).
- Every brand response has `Cache-Control: public, max-age=300`.
- An empty `BRAND_API_URL` disables the API; the static copies are then always
  used. Every slot has a static copy (checked by a test).

## Observability

- Structured JSON logs (`log/slog`) with request IDs.
- Prometheus metrics on `OPS_ADDR` (`/metrics`), plus `/healthz` and
  `/readyz` (dependency checks) on every service. The containers' healthcheck
  is `<binary> healthcheck`, so distroless images need no curl.
- Workers publish heartbeats to Redis, which the dashboard shows as service health.
- API analytics (public `/api/v1` traffic only) are aggregated hourly in Postgres:
  counts, status codes and a latency histogram for p50/p95/p99. Unique
  clients are counted per day with a Redis HyperLogLog of salted IP hashes,
  so no raw IPs are stored. Data is pruned after 90 days.

## Security notes

- Secrets are read from the environment and unset after loading (`unset` tag).
- Containers run as non-root on distroless images (read-only root filesystem
  and no capabilities in the VPS compose). Postgres, Redis and gRPC are never
  exposed.
- The client IP trust model is explicit (`TRUSTED_PROXY_HOPS` /
  `CLIENT_IP_HEADER`), so `X-Forwarded-For` can't be spoofed into the rate limiter.
- `GET /api/v1/now-playing/stream` (SSE) holds one Redis subscription per
  stream, so streams are capped: at most 6 per client IP (429) and 256 in all
  (503); each stream closes after about 30 minutes and clients reconnect.
- Lyrics reach the public only through `GET /api/v1/now-playing/lyrics`: the
  full lyrics and translation of the track playing now, `no-store`; it is
  outside `v1Routes`, so MCP cannot call it.
- The Spotify queue is private: it is read only to prefetch, kept in Redis
  for five minutes and shown only in the admin overview. A track that was
  only prefetched is not found on the public `GET /api/v1/tracks/{id}`
  (found once it has a play, or while it is the current track), so the queue
  cannot be probed from outside.
- Lyrics translation sends the lyrics of played and upcoming songs (title, artist and
  text) to the Anthropic API. The key is read once from `ANTHROPIC_API_KEY`
  (removed from the environment), lives only in the SDK client, and is never
  logged or stored; errors are stored without response bodies.
