# Legal, privacy and EU data protection

TRACES is self-hosted software. The person or organisation running an instance is
the data controller for that instance; the project itself does not operate a
central service. This document explains the legal surfaces the code provides and
what an operator must configure before publishing the Android app or opening the
instance to other people.

## Public legal pages

Three public pages are served straight from `static/` by `internal/web/legal.go`:

| Route | File | Purpose |
|---|---|---|
| `GET /privacy` | `static/privacy.html` | Privacy policy (GDPR information duties) |
| `GET /terms` | `static/terms.html` | Terms of use |
| `GET /account/delete` | `static/account_delete.html` | Public data-deletion page required by Google Play |

They never require authentication. `internal/web/legal.go` substitutes the
operator identity into these placeholders before serving:

| Placeholder | Environment variable | Fallback |
|---|---|---|
| `{{LEGAL_CONTROLLER_NAME}}` | `LEGAL_CONTROLLER_NAME` | `the operator of this TRACES instance` |
| `{{LEGAL_CONTROLLER_EMAIL}}` | `LEGAL_CONTROLLER_EMAIL` | `privacy@example.invalid` |
| `{{LEGAL_CONTROLLER_ADDRESS}}` | `LEGAL_CONTROLLER_ADDRESS` | `Not configured by the instance operator` |
| `{{LEGAL_HOSTING_REGION}}` | `LEGAL_HOSTING_REGION` | `European Union` |

**Launch blocker:** the fallbacks are deliberately non-identifying. Before
submitting to Google Play or inviting users, set all four variables to the real
controller identity and a working contact address. A policy that names an
`example.invalid` address is not a valid privacy notice under Art. 13 GDPR.

The pages are plain HTML and work without JavaScript. They describe the data the
server can store; keep them accurate if you enable the optional integrations
listed below.

## Account deletion

The same deletion service backs every entry point:

| Entry point | Route | Notes |
|---|---|---|
| Android app | Settings → Data & privacy → Delete my account | Requires typing `DELETE`; calls `DELETE /api/me` |
| HTTP API | `DELETE /api/me` with `{"confirm":"DELETE"}` | Used by the app; CSRF token required |
| Web session | `POST /settings/account/delete` | Form-friendly variant, CSRF protected |
| Admin panel | `DELETE /api/users?id=<id>` | Admin-only; reuses the same hard delete |

`internal/auth/account.go` performs the deletion in one transaction:

- deletes the user's `timeline_events` and their `collection_events` links;
- deletes the user's `event_templates`;
- deletes `password_reset_tokens` and all `sessions` for the user (cache and
  table), so every device is signed out;
- deletes the `users` row;
- after commit, removes the stored media files referenced by the user's events
  (full file, thumbnail and generated variants) from the media directory.

What is intentionally **kept**:

- `persons` records are instance-wide and have no owner column; another account
  may reference the same person, so deleting one account does not remove them;
- other users' events and media are untouched;
- rotated backups may still contain the data until they expire (see below);
- HTTP access logs and OpenTelemetry exports are outside the database and follow
  the operator's own retention.

Guards: the legacy root administrator (`users.id = 0`, stored in `admin_users`)
cannot be deleted, and deleting the last remaining administrator returns
`409` so an instance cannot lock itself out.

## Data export (right to portability)

`GET /api/me/export` returns a JSON attachment scoped to the caller:

- account profile (id, username, display name, email, colour, avatar, auth
  method, creation date, admin flag);
- the caller's timeline events, including locations and media URLs;
- person records referenced by those events;
- the caller's event templates;
- server-side preferences (`analytics_opt_out`).

It deliberately excludes password hashes, OIDC client secrets, session
identifiers, password-reset token hashes and integration credentials.

The Android app exposes this under Settings → Data & privacy → Download my data
and offers a copy-to-clipboard sheet.

## Analytics opt-out

The only analytics integration is self-hosted Umami, and it is disabled unless an
administrator configures a URL and site ID. When it is enabled, signed-in users
can opt out:

- `GET /api/me/privacy` → `{"analytics_opt_out": false}`
- `PATCH /api/me/privacy` with `{"analytics_opt_out": true}`
- `GET /api/config` returns `umami_enabled: false` for an opted-out session, so
  the analytics script is never injected into that user's pages
- the Android switch lives in Settings → Data & privacy

There is no advertising SDK, no third-party analytics, and no cookie-consent
banner: the only cookies required for the service are the HttpOnly `session`
cookie and short-lived OIDC state / CSRF values. Umami itself is cookieless. If
you add a non-essential cookie or tracker, add a consent mechanism before
deploying it in the EU.

## Retention

| Data | Retention in the code |
|---|---|
| Account and memories | Until the account is deleted |
| Sessions | 24 hours from sign-in, then expired by the cleanup loop |
| Login-failure counters | 24 hours, in memory |
| Uploaded media | Deleted with the account |
| Backups | `backup_settings.retention_days`, default 7, pruned automatically |
| Access logs | Wherever the operator ships stdout; no fixed retention |
| OpenTelemetry signals | Only if the operator configures an exporter |

Document your own proxy/access-log retention and backup rotation; the privacy
page intentionally does not promise a number the software cannot enforce.

## GDPR launch checklist

1. Set `LEGAL_CONTROLLER_NAME`, `LEGAL_CONTROLLER_EMAIL`,
   `LEGAL_CONTROLLER_ADDRESS` and `LEGAL_HOSTING_REGION`.
2. Publish the privacy policy at `https://<host>/privacy` and link it from the
   Play listing.
3. Publish the deletion page at `https://<host>/account/delete` and enter the
   same URL in Play Console → App content → Data deletion.
4. Sign a data processing agreement with the hosting provider and any optional
   processor you enable (SMTP, Umami, OTLP collector, OIDC provider, Gotify,
   Immich, OpenStreetMap Nominatim).
5. Decide and write down backup and log retention, then align the privacy page.
6. Provide a working data-subject request channel: the in-app export/deletion
   flows plus `LEGAL_CONTROLLER_EMAIL`.
7. Keep a short record of processing activities (purposes, categories, recipients,
   retention, safeguards). For a single instance this can be one page in your
   own documentation.
8. If a processor is outside the EEA, rely on an adequacy decision or Standard
   Contractual Clauses and mention it in the policy.
9. Confirm the instance is served over HTTPS. The Android client still permits
   cleartext for LAN self-hosting; public instances must not rely on it.
10. Re-read `static/privacy.html` whenever you enable a new integration, and
    update the "Last updated" date.

## Security measures referenced by the policy

- bcrypt password hashing (`golang.org/x/crypto/bcrypt`);
- HttpOnly, SameSite=Lax session cookies, `Secure` when the request is HTTPS;
- CSRF tokens for mutating browser requests;
- per-user scoping of events, exports and deletions;
- admin-only routes behind `RequireAdmin`;
- media deletion restricted to the configured media directory with path-traversal
  checks;
- secrets read from the environment, never logged.

## Children

The app and the hosted instance are not directed at children under 16. The Play
listing should declare an adult target audience; do not opt in to the Families
programme.

## Related documents

- [`docs/play-store.md`](play-store.md) — Play Console declarations and review
  instructions.
- [`static/privacy.html`](../static/privacy.html) — the user-facing policy.
