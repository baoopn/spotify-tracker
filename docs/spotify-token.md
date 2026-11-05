# Spotify authorization

The API owns the Spotify credentials. Workers only receive short-lived access
tokens over gRPC.

## One-time app setup

1. In the [Spotify Developer Dashboard](https://developer.spotify.com/dashboard),
   open (or create) the app and copy the **Client ID** and **Client Secret**
   into `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET`.
2. Under **Redirect URIs** add, exactly:
   - production: `https://<api domain>/admin/api/spotify/callback`
   - local: `http://127.0.0.1:8080/admin/api/spotify/callback`

   Spotify no longer accepts `localhost`. Plain HTTP is allowed only for
   loopback addresses (`127.0.0.1` / `[::1]`).
3. The app can stay in **Development Mode** (personal use). The app owner
   needs an active Premium subscription for Development Mode apps to keep
   working.

## Authorize / re-authorize (recommended)

Admin dashboard → **Settings → Spotify → Connect / Re-authorize**. Approve the
consent screen and you're sent back to the dashboard. The new refresh token is
stored encrypted, and the poller uses it immediately; no restart needed.

Scopes requested: `user-read-currently-playing`, `user-read-playback-state`,
`user-read-recently-played`.

## Token lifetime

Since mid-2026, Spotify refresh tokens **expire 180 days after authorization**
(the token endpoint then answers `invalid_grant`). The dashboard shows the
days left. Alert emails go out 14, 7, 3 and 1 days before expiry, on expiry,
and when Spotify rejects the token. Re-authorize from the dashboard when they arrive.

## Seeding a token from the environment

An existing refresh token can be provided as `SPOTIFY_REFRESH_TOKEN`, with
`SPOTIFY_AUTHORIZED_AT=YYYY-MM-DD` so expiry warnings are accurate. It's
copied into the database on start, and again whenever the env value changes.
After that the dashboard flow is the source of truth.

## Manual flow (fallback)

```bash
# 1. Open in a browser and approve (replace CLIENT_ID):
https://accounts.spotify.com/authorize?client_id=CLIENT_ID&response_type=code&redirect_uri=http%3A%2F%2F127.0.0.1%3A8888%2Fcallback&scope=user-read-currently-playing%20user-read-playback-state%20user-read-recently-played
# 2. Copy ?code=… from the redirect and exchange it:
curl -u "CLIENT_ID:CLIENT_SECRET" -d grant_type=authorization_code -d code=CODE \
     -d redirect_uri=http://127.0.0.1:8888/callback https://accounts.spotify.com/api/token
```

Put the returned `refresh_token` into `SPOTIFY_REFRESH_TOKEN` and today's date
into `SPOTIFY_AUTHORIZED_AT`. The redirect URI must be registered for the app.
