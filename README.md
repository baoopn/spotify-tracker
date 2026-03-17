# Baoopn Spotify Tracker

A small Deno monorepo that polls Spotify for "now playing" information, stores history and track metadata in Directus, and enriches tracks with audio features.

Stack
- Runtime: Deno
- Database / CMS: Directus
- Frontend: SvelteKit (not implemented yet)

Workspace layout
- `apps/worker-poll` — polls Spotify (`/src/poller.ts`) and writes current listening & history to Directus
- `apps/worker-enrich` — enriches tracks with audio features from external APIs
- `apps/frontend` — SvelteKit app (public display; not implemented yet)
- `packages/shared` — shared Zod schemas and tRPC/router definitions

Frontend status
- The frontend is not implemented yet. When added it will publicly display your current listening track and visualizations of recent listening tendencies: genres breakdown and top tracks.

Quick start (development)
1. Ensure Directus is available (see `directus/docker-compose.coolify.yml`).
2. Configure Spotify tokens and Directus credentials — see `docs/spotify-token.md` and `docs/directus-collections-setup.md` for guidance.
3. Run the poller and enricher workers from the repo root:

```powershell
deno task dev:worker-poll
deno task dev:worker-enrich
```

Notes
- Worker polling interval and behaviour are implemented in `apps/worker-poll` and `apps/worker-enrich`.
- All shared schemas live in `packages/shared/schemas.ts` and use Zod for strict typing.

More information
- See `docs/spotify-token.md` and `docs/directus-collections-setup.md` for tokens and Directus collection setup.

