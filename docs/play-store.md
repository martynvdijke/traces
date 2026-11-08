# Google Play submission notes

Operational checklist for publishing the TRACES Android app. Play Console labels
change occasionally; the values below are the ones the build and the hosted
instance actually support.

## Required URLs

| Play Console field | Value |
|---|---|
| Privacy policy | `https://traces.vandijke.xyz/privacy` |
| Account deletion | `https://traces.vandijke.xyz/account/delete` |
| App access → test account | see below |

Both pages are public, require no login, and must stay reachable for as long as
the app is listed. The deletion URL must explain *what* is deleted, not just link
to a form — `static/account_delete.html` does that.

## App access (reviewer account)

The app talks to a self-hosted server and has no public sign-up, so Play review
needs credentials. The server provisions a demo account on startup:

| Variable | Default | Meaning |
|---|---|---|
| `DEMO_USER_ENABLED` | `true` only when `TRACES_PUBLIC_URL` host is `traces.vandijke.xyz`, else `false` | Provision the reviewer account |
| `DEMO_USER_USERNAME` | `demo` | Reviewer username |
| `DEMO_USER_PASSWORD` | `demo` | Reviewer password |
| `DEMO_USER_EMAIL` | `demo@traces.local` | Stored email (not a mailbox) |
| `DEMO_USER_DISPLAY_NAME` | `Demo User` | Display name |
| `DEMO_USER_SEED_CONTENT` | `true` | Add sample memories when the account has none |
| `TRACES_PUBLIC_URL` | — | Public base URL, used to decide the demo default |

The account is created idempotently on every start, is never an administrator,
and resets its password to the configured value if it drifts. If a reviewer
deletes it, the next server restart recreates it.

Enter in Play Console → App content → App access:

```
Username: demo
Password: <value of DEMO_USER_PASSWORD>
Instructions: Open the app, keep the prefilled server https://traces.vandijke.xyz,
tap "Try the demo account" (or sign in with the credentials above), then use the
Timeline, Map, Log a memory, Activity and Compare tabs.
```

The mobile app shows the "Try the demo account" button only when the server's
`GET /api/config` reports `demo_enabled: true`, so verify the deployed instance
has the demo account enabled before submitting.

## Data deletion declaration

Play requires both an in-app path and a web URL for account deletion:

- In app: Settings (gear) → Data & privacy → Delete my account → type `DELETE`.
- Web: `https://traces.vandijke.xyz/account/delete`, with an email fallback for
  users who cannot sign in.

The web page and the app both use the same server-side deletion service. Review
the exact semantics in [`docs/legal.md`](legal.md#account-deletion).

## Data safety answers

Answer for the hosted instance (`traces.vandijke.xyz`), not for every possible
self-hosted configuration.

| Category | Collected | Shared | Notes |
|---|---|---|---|
| Name | Yes | No | Display name, optional |
| Email address | Yes | No | Optional account field; OIDC may supply it |
| User IDs | Yes | No | Account identifier |
| Photos and videos | Yes | No | User uploads; stored on the user's server |
| Audio files | Yes | No | Voice notes, if used |
| Precise / approximate location | Yes | No | Stored on memories for the map; not continuous background tracking |
| Other user-generated content | Yes | No | Titles, descriptions, tags, people |
| App activity | Yes | No | Memories and interactions stored on the server |
| Diagnostics | Optional | No | Only if the operator enables OpenTelemetry |

- Data is **not** sold and **not** shared for advertising.
- Data is encrypted in transit when the instance is served over HTTPS.
- Users can request deletion in the app and via the web URL; deletion is
  available without contacting support.
- The app contains no advertising SDK and no third-party analytics SDK. The web
  UI can load a self-hosted Umami instance, which signed-in users can opt out of.

## Permissions and rationale

| Permission | Why | If denied |
|---|---|---|
| `INTERNET` | Talk to the self-hosted server | App unusable |
| `CAMERA` | Capture a photo/video for a memory | Pick an existing file instead |
| `ACCESS_FINE_LOCATION`, `ACCESS_COARSE_LOCATION` | One-shot geo-tag of a new memory; map centring | Memories can be saved without coordinates |
| `READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO`, `READ_MEDIA_AUDIO` | Choose existing media to upload | Capture in-app instead |
| `READ_EXTERNAL_STORAGE` (maxSdk 32) | Same, on older Android | Same |
| Notifications | Only requested when the user enables push | No notifications |

No background location permission is declared. Location is captured for a
specific memory, not tracked continuously.

`android:usesCleartextTraffic="true"` exists so people can point the app at a LAN
server without TLS. Public instances should use HTTPS; mention this in the review
notes if asked.

## Ads, content rating and audience

- Contains ads: **No**.
- In-app purchases: **No**.
- Target audience: **16+**; not directed at children, not in the Families
  programme.
- Content rating questionnaire: user-generated personal content (photos, text),
  no violence, sexual content, profanity, gambling or controlled substances.
  Expect Everyone/Teen depending on the questionnaire.
- Users can only see their own data plus content explicitly shared inside their
  own instance; there is no public social network.

## Build and signing

- The Play workflow builds an App Bundle: `flutter build appbundle --release`.
- Version comes from `mobile/pubspec.yaml` (`version: x.y.z+build`); the build
  number must increase for every upload.
- Signing requires `mobile/android/key.properties` and the keystore; CI uses the
  `ANDROID_KEYSTORE_*` secrets. Uploads use
  `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON` with `GOOGLE_PLAY_TRACK` and
  `GOOGLE_PLAY_RELEASE_STATUS`.
- Build with JDK 17. See `AGENTS.md` and `.github/workflows/play-store.yaml`.

## Testing track requirement

Personal developer accounts created after November 2023 must run a closed test
with at least 12 testers for 14 continuous days before production access. Start
that track early; it is independent of the code.

## Pre-submission checklist

1. `task prepush` passes (Go build/vet/tests, TypeScript, Flutter tests).
2. `LEGAL_*` variables point at a real controller identity and a working email.
3. `/privacy`, `/terms` and `/account/delete` return 200 on the production host.
4. `DEMO_USER_ENABLED=true` on the production host and the demo login works from
   a clean install with no saved server URL.
5. Deletion tested end to end with the demo account: app flow, web flow, and the
   email fallback.
6. Data safety answers match the deployed configuration (especially whether
   Umami/OTLP/SMTP are enabled).
7. The Play listing links the privacy policy and the deletion URL.
8. Screenshots show the real app (onboarding, timeline, map, capture), not the
   Flutter template.
