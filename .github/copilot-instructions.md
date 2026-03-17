# Project: Spotify Tracker (Deno Monorepo)

## 1. Stack & Architecture
- **Runtime:** Deno (Monorepo Workspace).
- **Backend/DB:** Directus (Storage & Auth).
- **Frontend:** SvelteKit + Ark UI + Tailwind CSS.
- **Communication:** tRPC for type-safe API calls between Workers/Frontend and Directus.
- **Validation:** Zod for schema validation (Spotify API responses & Directus payloads).

## 2. Workspace Structure
The project is organized as a Deno workspace:
- `./apps/frontend`: SvelteKit application.
- `./apps/worker-poll`: Deno service polling Spotify (5s interval).
- `./apps/worker-enrich`: Deno service fetching audio features from Soundcharts.
- `./packages/shared`: Zod schemas and tRPC router definitions shared across the monorepo.

## 3. Shared Logic (tRPC & Zod)
- Define all Spotify track and Audio Feature structures in `./packages/shared/schemas.ts` using Zod.
- Use tRPC to create a "Bridge" if the Frontend needs to trigger worker actions or fetch live state directly from the workers.
- Ensure Directus collection types are mirrored in Zod for strict internal typing.

## 4. Frontend (Svelte + shadcn-svelte)
- **Components:** Use shadcn-svelte (built on Bits UI primitives) for accessible, customizable UI components.
- **Data Fetching:** Utilize tRPC hooks to fetch "Now Playing" status and history from the Directus/Worker layer with full type safety.
- **Reactivity:** Implement Svelte 5 runes (or stores) to manage the real-time progress bar, using local interpolation to ensure smooth movement between the 5-second worker polls.

## 5. Worker Logic (Deno)
### Worker A: Poller
- Polls `https://api.spotify.com/v1/me/player/currently-playing` every 5 seconds.
- Uses Zod to parse Spotify responses before hitting Directus.
- Maintains local state for `last_track_id` to prevent redundant History inserts.
### Worker B: Enricher
- Listens to Directus via Webhooks or polls every 10 minutes for `enriched == false`.
- Fetches from Soundcharts and populates the `track_features` collection.

## 6. Coding Standards
- **Imports:** Use `deno.json` aliases (e.g., `@shared/schemas`) to reference monorepo packages.
- **Environment:** Access variables via `Deno.env.get()`.
- **Typing:** NO `any`. Every Spotify response and Directus record must be typed with Zod/TypeScript interfaces.
- **Directus:** Use the `@directus/sdk` (via npm specifier) for interacting with the database.

## 7. Directus Schema Reminder
- **Collections:** `current_listening` (Singleton), `listening_history` (Log), `track_features` (Metadata).
- Ensure `track_id` is the primary key link between history and features.