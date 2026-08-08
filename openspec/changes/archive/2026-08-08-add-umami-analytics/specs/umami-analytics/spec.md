## ADDED Requirements

### Requirement: Admin can configure Umami analytics URL and site ID
The system SHALL provide an admin user interface to configure a self-hosted Umami analytics instance URL and website ID. Configuration SHALL be persisted in a database table and exposed via REST API endpoints.

#### Scenario: Admin opens Umami config form
- **WHEN** an authenticated admin navigates to the Integrations tab
- **THEN** the Umami analytics card is visible with URL, Site ID fields, and an Enable toggle

#### Scenario: Admin saves Umami config
- **WHEN** the admin fills in URL, Site ID, and toggles Enable, then clicks Save
- **THEN** the configuration is persisted to the `umami_settings` table
- **AND** the Umami analytics script is loaded on subsequent page views

#### Scenario: Saved config persists across server restart
- **WHEN** the admin saves Umami config, then the server is restarted
- **THEN** the saved values are loaded from the database and used

#### Scenario: Env vars provide startup defaults
- **WHEN** `UMAMI_URL` and `UMAMI_SITE_ID` environment variables are set at server start
- **THEN** the globals `umamiURL` and `umamiSiteID` are initialized from these env vars
- **AND** if no DB row exists, the env var values become the initial config

### Requirement: Public timeline loads Umami analytics script
The public-facing timeline page (index.html) SHALL dynamically load the Umami analytics script when both URL and Site ID are configured and analytics are enabled.

#### Scenario: Analytics load on public timeline
- **WHEN** Umami URL and Site ID are configured and enabled
- **THEN** the public timeline page loads `<script async defer src="{url}/script.js" data-website-id="{site_id}"></script>`

#### Scenario: Analytics not loaded when disabled
- **WHEN** Umami is configured but disabled (enabled=false)
- **THEN** the analytics script is NOT loaded on any page

### Requirement: Admin panel loads Umami analytics script
The admin panel page (admin.html) SHALL also load the Umami analytics script when configured, via the `loadAdminAnalytics()` function called from `init()`.

#### Scenario: Analytics load on admin panel
- **WHEN** an authenticated admin visits admin.html and Umami is configured and enabled
- **THEN** the Umami analytics script is injected into the page head

### Requirement: API exposes Umami config for public consumption
The existing `GET /api/config` endpoint SHALL continue to expose `umami_url` and `umami_site` values for use by frontend pages.

#### Scenario: Public config returns Umami settings
- **WHEN** any client calls `GET /api/config`
- **THEN** the response includes `umami_url` and `umami_site` fields (may be empty strings if not configured)

### Requirement: Backend manages Umami config via REST API
Authenticated admin users SHALL be able to read and update Umami configuration via dedicated API endpoints following the Gotify/Immich pattern.

#### Scenario: GET /api/umami/config returns current config
- **WHEN** an authenticated admin calls `GET /api/umami/config`
- **THEN** the response contains `url`, `site_id`, and `enabled` fields

#### Scenario: POST /api/umami/config persists and applies config
- **WHEN** an authenticated admin calls `POST /api/umami/config` with valid JSON body `{url, site_id, enabled}`
- **THEN** the values are saved to the `umami_settings` table
- **AND** the global variables `umamiURL`, `umamiSiteID` are updated in-memory
- **AND** the response is `{"status": "ok"}`
