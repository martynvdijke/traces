## Context

TRACES combines browser sessions, admin authorization, CSRF-sensitive routes, and machine-readable timeline endpoints. Bearer tokens must be an additional machine credential, not a bypass around user or role checks.

## Goals / Non-Goals

### Goals

- Keep public TRMNL summary reads available.
- Protect administrative, upload, import, and mutation routes with user plus token.
- Preserve browser session, role, and CSRF behavior.

### Non-Goals

- Making the summary endpoint private.
- Replacing browser login or admin roles.
- Committing production secrets.

## Decisions

- Add a reversible token table containing owner, hash, expiry, timestamps, and revocation state.
- Validate bearer tokens after session authentication and before mutation handlers; require role checks separately.
- Add owner-only lifecycle endpoints with one-time secret output.
- Exclude secrets from structured logs, audit payloads, and error messages.

## Risks / Trade-offs

Existing upload and automation clients need migration. The production summary route must be checked separately because deployment drift previously caused a 404.

## Migration Plan

1. Add persistence and token lifecycle operations.
2. Audit and protect admin/upload/mutation routes.
3. Add public-read, auth, ownership, role, CSRF, and secret-redaction tests.
4. Provision client tokens and monitor rejected legacy calls.

## Open Questions

- Should tokens support route scopes for upload-only clients?
