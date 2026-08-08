## 1. Database & Backend Schema

- [x] 1.1 Bump `currentSchemaVersion` from 17 to 18 in `main.go`
- [x] 1.2 Add `CREATE TABLE IF NOT EXISTS umami_settings` to `initDB()` with columns: `id INTEGER PRIMARY KEY CHECK (id = 1)`, `url TEXT DEFAULT ''`, `site_id TEXT DEFAULT ''`, `enabled INTEGER DEFAULT 0`
- [x] 1.3 Add migration case 18: create `umami_settings` table and insert default row
- [x] 1.4 Add default row insertion after migration in `initDB()` (follow gotify/immich pattern)
- [x] 1.5 Add `UmamiConfig` struct type in `main.go` (fields: URL, SiteID, Enabled)

## 2. Backend API Handlers

- [x] 2.1 Implement `getUmamiConfig` handler: query `umami_settings` table, return JSON
- [x] 2.2 Implement `saveUmamiConfig` handler: bind JSON, UPDATE `umami_settings`, update globals `umamiURL`/`umamiSiteID`, log event
- [x] 2.3 Register routes in authenticated group: `GET /api/umami/config` and `POST /api/umami/config`
- [x] 2.4 Update `main()` to load `umami_settings` from DB if env vars not set (same as Immich pattern at lines 337-345)
- [x] 2.5 Update `getPublicConfig()` to reference the runtime globals (already does this)

## 3. Admin Panel UI (HTML)

- [x] 3.1 Add Umami settings card in the Integrations tab of `admin.html`, positioned after Immich card and before backup card, with: URL input, Site ID input, Enable toggle, Save button

## 4. Admin Panel TypeScript

- [x] 4.1 Add `loadUmamiConfig()` function in `ts/admin.ts` that fetches `/api/umami/config` and populates the form fields
- [x] 4.2 Add form submit handler for the Umami form that POSTs to `/api/umami/config`
- [x] 4.3 Call `loadUmamiConfig()` in `init()` function
- [x] 4.4 Call `loadAdminAnalytics()` in `init()` so the admin panel itself loads analytics

## 5. Public Frontend Analytics

- [x] 5.1 Add `loadAnalytics()` function in `ts/app.ts` that fetches `/api/config` and dynamically injects the Umami script tag (use `async` and `defer` attributes)
- [x] 5.2 Call `loadAnalytics()` during app initialization in `initApp()`

## 6. Compile & Verify

- [x] 6.1 Run `npx tsc` to check TypeScript compilation
- [x] 6.2 Run `go build -o traces-server .` to check Go compilation
- [x] 6.3 Run `go test ./...` to verify existing tests still pass
