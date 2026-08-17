## Why

TRACES has public timeline summary reads and many administrative/upload mutations. Existing browser authentication must be extended for machine clients without weakening sessions or CSRF.

## What Changes

- Keep `GET /api/trmnl/summary` and explicitly public reads unauthenticated.
- Require an authenticated user and bearer API token for admin, upload, import, and all create/update/delete operations.
- Add one-time-visible, hashed, revocable user tokens.

## Capabilities

### New Capabilities

- `api-authentication`

### Modified Capabilities

- `public-api-reads`

## Impact

TRACES auth middleware, admin/upload handlers, persistence migration, token management, tests, and API documentation change. Browser session and CSRF behavior must remain intact.
