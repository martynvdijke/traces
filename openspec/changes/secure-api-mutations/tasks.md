## 1. Route audit and storage

- [ ] 1.1 Inventory admin, upload, import, tag/trash, and other mutation routes with role/CSRF requirements.
- [ ] 1.2 Add reversible hashed-token migration and indexes.

## 2. Authentication

- [ ] 2.1 Implement bearer validation, expiry, revocation, and owner matching.
- [ ] 2.2 Compose token checks with session, role, and CSRF middleware.
- [ ] 2.3 Implement owner-only create/list/revoke/rotate operations.

## 3. Verification and rollout

- [ ] 3.1 Test public summary reads and every mutation failure mode.
- [ ] 3.2 Test role boundaries, ownership, malformed/expired/revoked tokens, and secret redaction.
- [ ] 3.3 Document client migration and run the full TRACES test suite.
