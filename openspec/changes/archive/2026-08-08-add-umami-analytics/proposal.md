## Why

Umami is a privacy-focused, self-hosted analytics platform. The TRACES codebase already has partial wiring for Umami (env vars `UMAMI_URL` and `UMAMI_SITE_ID`, and a `/api/config` endpoint exposing them), but there is no admin panel UI to configure these settings, no database persistence, and the public frontend does not load the Umami analytics script. Adding a proper admin configuration panel and wiring the public frontend will allow self-hosted instances to opt into analytics without editing environment variables, making the feature accessible to non-technical admins.

## What Changes

- Add a new `umami_settings` database table for persisting Umami configuration
- Add new `GET /api/umami/config` and `POST /api/umami/config` API endpoints (following the Gotify/Immich pattern)
- Add an Umami configuration card in the Integrations tab of the admin panel
- Add a `loadUmamiConfig()` function and form save handler in the admin TypeScript source
- Call `loadAdminAnalytics()` from `init()` in `ts/admin.ts` (currently defined but never called)
- Ensure the public frontend (`index.html` via `ts/app.ts` / `static/app.ts`) loads the Umami script when configured
- Remove the dead `loadAdminAnalytics()` from admin.ts since the public frontend will handle analytics loading
- Maintain backward compatibility with existing env-var-based configuration (`UMAMI_URL` and `UMAMI_SITE_ID`)

## Capabilities

### New Capabilities
- `umami-analytics`: Configuration and injection of self-hosted Umami analytics across both the admin panel and public timeline pages

### Modified Capabilities

None — no existing specs to modify.

## Impact

- **Backend**: New `umami_settings` table, new Go struct `UmamiConfig`, new API handlers `getUmamiConfig`/`saveUmamiConfig`, migration case 17, default row insertion in `initDB()`
- **Frontend**: New HTML card in Integrations tab of `admin.html`, new `loadUmamiConfig()` + save handler in `ts/admin.ts`, Umami script injection in `ts/app.ts` (public frontend)
- **Env Vars**: Existing `UMAMI_URL` and `UMAMI_SITE_ID` continue to work as defaults
- **Tests**: New Playwright e2e tests for Umami config CRUD in `tests/admin.spec.ts`
