# Admin dashboard

Served by the API at **`/admin`** (SvelteKit SPA embedded in the binary).

Features:

- **Overview:** live now-playing, listening KPIs, Spotify token status (days left), service health from heartbeats, enrichment progress.
- **Data:** history, tracks and artists browsers with search, sorting and filters. Track details show audio features and genres; tracks can be re-enriched, and failed enrichments retried in bulk.
- **Analytics:** public API traffic (requests, error rate, p50/p95/p99, unique clients, routes, status codes, origins) plus listening insights.
- **Settings:** Spotify connect / re-authorize / test, SMTP and alert settings with a test email, appearance (pastel palettes, light/dark/system).
- **Audit log** of admin actions.

API contract: [admin-api.md](admin-api.md).

## Authentication (Clerk)

1. Create an application at [clerk.com](https://clerk.com). Pick the sign-in
   methods you want (email + passkeys, Google…). For a private dashboard,
   restrict sign-ups (Configure → Restrictions → *Allowlist* or disable sign-ups).
2. **Development instance:** copy the `pk_test_…` / `sk_test_…` keys into `.env`:
   ```env
   CLERK_PUBLISHABLE_KEY=pk_test_…
   CLERK_SECRET_KEY=sk_test_…
   ADMIN_ALLOWED_EMAILS=you@example.com
   ```
3. **Production instance:** create it in Clerk, set the application domain
   to your API domain (Clerk asks for DNS records, e.g. `clerk.<domain>`
   CNAMEs), then put the `pk_live_…` / `sk_live_…` keys into the Coolify
   environment.
4. Access is granted to users in `ADMIN_ALLOWED_EMAILS` (verified emails),
   `ADMIN_ALLOWED_USER_IDS`, or whose Clerk `public_metadata.role` equals
   `ADMIN_ROLE` (default `admin`; public metadata can only be set server-side).
   Everyone else gets *403 forbidden* and the attempt is audited.
5. Session token claims (Configure → Sessions → Customize session token), on
   both instances:
   ```json
   { "role": "{{user.public_metadata.role}}", "email": "{{user.primary_email_address}}" }
   ```
   The API prefers these claims (fresh with every ~60 s token) over its cached
   profile lookup, and can still authorize from them if Clerk's Backend API is
   briefly unreachable.

The publishable key is served at runtime from `GET /admin/api/config`, so the
same image works for every environment. The CSP automatically allows the
Clerk Frontend API host encoded in the key.

## Alert emails

Alerts (Spotify token expiring or expired, Spotify rejecting the login) and the
Settings → Email test email are sent as `multipart/alternative`: a plain-text
part and a branded HTML part (table layout, inline styles, light and dark mode).
The layout lives in `internal/notify/templates/email.html.tmpl`; the content of
each alert is in `internal/alerts/emails.go`.

- The header logo is `web/public/static/email/listening-wordmark-primary@2x.png`,
  served at `<PUBLIC_URL>/email/listening-wordmark-primary@2x.png`. Set
  `EMAIL_LOGO_URL` to load it from somewhere else.
- Preview every email (HTML, text and raw `.eml`; nothing is sent):
  `go run ./cmd/emailpreview -out ./email-preview -public-url https://listening.example.com`
- Golden files are in `internal/notify/testdata/`. After changing a template or
  an alert, run `go test ./internal/notify -run TestGolden -update` and review the diff.

## Local development

```bash
make deps && make run-api   # API on :8080
make web-dev                # http://localhost:5173/admin with hot reload
```

`APP_ENV=development` additionally accepts Clerk tokens minted for
`http://localhost:5173`.
