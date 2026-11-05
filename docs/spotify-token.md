# Spotify developer setup & authorization

The API owns the Spotify credentials (client secret + encrypted refresh
token); the poller only receives short-lived access tokens over gRPC.

## 1. Requirements (Spotify rules as of 2026)

- **Spotify Premium is required.** Since the February 2026 Web API changes,
  the owner of a Development Mode app must have an **active Premium
  subscription**; if it lapses, the app's API access stops working (the
  poller will log authorization errors and alert by email).
- **Development Mode is enough** for this personal tracker: up to **5
  users** can be added to an app (only the owner is needed here). Extended
  Quota Mode (public apps) requires a separate review by Spotify and is not
  needed.
- One developer account can own **up to 25 apps** (raised from 1 in July 2026).
- **Quotas are counted per developer account**, not per app. A 429 response
  may include `reason: QUOTA_EXCEEDED`; the poller backs off automatically
  using `Retry-After`.
- **Redirect URIs** must match exactly. `localhost` is **not** accepted;
  plain HTTP is allowed only for loopback addresses (`http://127.0.0.1:PORT`
  or `http://[::1]:PORT`); everything else must be HTTPS.
- **Refresh tokens expire 180 days after authorization** (shown as "Refresh
  Token Lifetime" in the dashboard). Refreshing does not extend this; you
  must re-authorize. See [Token lifetime](#4-token-lifetime--re-authorizing).
- Some data is no longer available to Development Mode apps (audio features,
  recommendations, artist popularity/followers, Spotify-generated playlists
  such as Daily Mix). This project gets audio features and genres from
  **Soundcharts** instead and shows Spotify mixes as "unavailable".

## 2. Create the developer app (one time)

1. Make sure the Spotify account you will track has **Premium**.
2. Sign in at the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard)
   with that account and accept the Developer Terms if asked.
3. **Create app**:
   - *App name / description*: e.g. `baoopn listening`.
   - *Redirect URIs* (add all you need, exactly):
     - production: `https://listening.baoopn.com/admin/api/spotify/callback`
     - local dev: `http://127.0.0.1:8080/admin/api/spotify/callback`
   - *APIs used*: **Web API**.
   - Accept the terms and save.
4. Open the app → **Settings**: copy the **Client ID** and **Client
   secret** into `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET` (local `.env`,
   and the Coolify resource's environment for production).
5. *(Optional)* **User Management**: only needed if someone other than the
   owner should authorize the app (max 5 users in Development Mode).

## 3. Authorize (recommended: from the dashboard)

Admin dashboard → **Settings → Spotify → Connect / Re-authorize**. Approve the
consent screen with the Premium account; you are redirected back and the new
refresh token is stored encrypted (`SECRETS_KEY`). The poller picks it up
immediately — no restart.

Scopes requested:

| Scope | Why |
|---|---|
| `user-read-currently-playing` | now playing (required) |
| `user-read-playback-state` | playback state details |
| `user-read-recently-played` | recently played |
| `playlist-read-private`, `playlist-read-collaborative` | names/covers of private & collaborative playlists you play from |

If an older token lacks the playlist scopes, Settings → Spotify shows a
"Re-authorize to include private playlists" hint.

Optional hardening: set `SPOTIFY_OWNER_ID` to your Spotify user ID (shown in
Settings → Spotify after connecting) so only that account can ever be
connected.

## 4. Token lifetime & re-authorizing

- The dashboard shows the expiry date and days left.
- Alert emails are sent 14, 7, 3 and 1 day(s) before expiry, on expiry, and
  immediately if Spotify rejects the token (`invalid_grant`), each with a
  direct link to Settings → Spotify. Recipients and SMTP settings are in
  Settings → Email.
- To renew: click **Re-authorize** — that's all.

## 5. Seeding a token from the environment (migrations / headless setups)

An existing refresh token can be provided as `SPOTIFY_REFRESH_TOKEN`, with
`SPOTIFY_AUTHORIZED_AT=YYYY-MM-DD` so expiry warnings are accurate. It is
copied into the database on start and whenever the env value changes; after
that the dashboard flow is the source of truth.

### Manual flow (fallback)

```bash
# 1. Open in a browser and approve (replace CLIENT_ID; the redirect URI must
#    be registered for the app):
https://accounts.spotify.com/authorize?client_id=CLIENT_ID&response_type=code&redirect_uri=http%3A%2F%2F127.0.0.1%3A8888%2Fcallback&scope=user-read-currently-playing%20user-read-playback-state%20user-read-recently-played%20playlist-read-private%20playlist-read-collaborative
# 2. Copy ?code=… from the redirect (the page may fail to load — that's fine)
#    and exchange it:
curl -u "CLIENT_ID:CLIENT_SECRET" -d grant_type=authorization_code -d code=CODE \
     -d redirect_uri=http://127.0.0.1:8888/callback https://accounts.spotify.com/api/token
```

Put the returned `refresh_token` into `SPOTIFY_REFRESH_TOKEN` and today's date
into `SPOTIFY_AUTHORIZED_AT`.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `invalid_grant` in logs, alert email | Token expired (180 days) or revoked → Re-authorize. |
| 403 / app suddenly stops working | Owner's Premium lapsed, or the user isn't in the app's User Management list. |
| `INVALID_CLIENT: Invalid redirect URI` | The callback URL isn't registered exactly (scheme, host, port, path). |
| 429 `QUOTA_EXCEEDED` | Account-level quota; the poller backs off automatically. Raise `POLL_INTERVAL_MS` if it persists. |
| Playlist shows "Spotify mix / unavailable" | Spotify-generated playlists aren't readable by Development Mode apps. |

Sources: Spotify Web API changelogs
([Feb 2026](https://developer.spotify.com/documentation/web-api/references/changes/february-2026),
[Mar 2026](https://developer.spotify.com/documentation/web-api/references/changes/march-2026),
[Jul 2026](https://developer.spotify.com/documentation/web-api/references/changes/july-2026)),
[Feb 2026 migration guide](https://developer.spotify.com/documentation/web-api/tutorials/february-2026-migration-guide),
[refresh-token expiration](https://developer.spotify.com/blog/2026-06-18-refresh-token-expiration),
[redirect URI rules](https://developer.spotify.com/documentation/web-api/concepts/redirect_uri).
