## Context

TRACES has existing environment-variable-based Umami analytics wiring (`UMAMI_URL` and `UMAMI_SITE_ID` globals loaded in `main()`, exposed via `GET /api/config`). Several frontend pages (login, setup) already have inline scripts that load the Umami tracker from `/api/config`. However:

- **No database persistence**: Umami config cannot be changed at runtime
- **No admin panel UI**: Admins must restart with env vars to configure
- **Admin panel itself** has a `loadAdminAnalytics()` function in `ts/admin.ts` that is defined but never called
- **Public timeline** (`index.html` / `ts/app.ts`) does not load Umami at all
- No test endpoint exists to validate the Umami connection

The codebase has a well-established pattern for persisting integration configs: a single-row settings table + `GET/POST /api/<name>/config` endpoints + a card in the Integrations tab of the admin panel (see Gotify, Immich, Ollama, Email).

## Goals / Non-Goals

**Goals:**
- Persist Umami settings in a `umami_settings` table (single-row, id=1)
- Expose `GET /api/umami/config` and `POST /api/umami/config` endpoints
- Add an Umami config card in the admin Integrations tab
- Load Umami analytics on the public frontend when configured
- Load Umami analytics on the admin panel when configured
- Support env-var-based config as initial defaults (env vars override at startup only, DB writes take precedence at runtime)
- Include a schema migration (version 17)

**Non-Goals:**
- Umami e-commerce or custom event tracking
- Multiple Umami instances/sites
- Umami Cloud (only self-hosted; Cloud uses a different script URL pattern)
- Test connection endpoint (Umami's script.js is a static file, no API to test against)

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Pattern to follow | Gotify/Immich single-row settings table | Established pattern: `gotify_settings`, `immich_settings`, `ollama_settings` all use single-row tables with `id INTEGER PRIMARY KEY CHECK (id = 1)` |
| Table columns | `url TEXT`, `site_id TEXT`, `enabled INTEGER` | Matches the existing `UmamiConfig` data shape; `enabled` allows toggling without clearing values |
| Env var precedence | At startup, env vars override DB. After admin saves, DB is source of truth | Same as how `IMMICH_URL`/`IMMICH_API_KEY`/`IMMICH_ENABLED` work currently |
| Global vars vs DB reads | Keep `umamiURL`/`umamiSiteID` globals, reload on save | Matches Gotify pattern (`gotifyURL = cfg.URL` after save); avoids per-request DB reads |
| Analytics loading strategy | Call `/api/config` from both admin panel and public frontend, dynamically inject `<script>` tag | Already how login.ts and setup.ts work; no build-time dependency needed |
| Schema version | Bump `currentSchemaVersion` from 17 to 18 | Version 17 is the current latest |

## Risks / Trade-offs

- **Race condition on startup**: If env vars and DB differ, env wins on first start. Users who set env vars then change via admin panel will see the admin value until restart. This matches existing Immich/Gotify behavior.
- **No test endpoint**: Unlike Gotify (which has a push API) and Immich (which has a /server-info/version endpoint), Umami's script.js just serves JS. A test would be a simple HTTP GET to the script URL. Consider adding a lightweight `GET /api/umami/test` that fetches `${url}/script.js` and checks for a 200 response.
- **`loadAdminAnalytics()` is dead code**: Currently defined in `ts/admin.ts` but never called. After this change it should be called from `init()`. The function name is misleading — it loads analytics for the admin panel page itself, not for configuring analytics.

## Open Questions

- Should the public frontend analytics loading be gated on `publicMode`? Currently the frontend is always public-facing, so analytics should load on all pages where the timeline is viewed. Keeping it unconditional when configured.
- Should the analytics script be loaded with `async` and `defer`? Yes (already the case in the existing function) to avoid blocking page rendering.
