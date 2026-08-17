## ADDED Requirements

### Requirement: Public timeline reads

The service SHALL allow unauthenticated GET requests to `/api/trmnl/summary` and documented public timeline reads.

#### Scenario: Public TRMNL summary

- WHEN TRMNL requests the summary without credentials
- THEN the service returns the public summary payload

### Requirement: Administrative mutations require user and token

Admin events, collections, templates, users, tags/trash, uploads, imports, and every create/update/delete API operation MUST require an authenticated user plus a valid bearer token, while retaining existing role checks and CSRF protections.

#### Scenario: Token-only upload

- WHEN a client submits an upload with a valid token but no user session
- THEN the service rejects it without storing the upload

### Requirement: Token lifecycle is secure

Users SHALL create, list metadata for, revoke, and rotate their own tokens. The secret MUST be shown once, stored only as a hash, and excluded from logs and later responses.

#### Scenario: Other-user token

- WHEN a user presents a token owned by another user
- THEN the mutation is rejected without disclosing ownership
