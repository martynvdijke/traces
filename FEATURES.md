# TRACES — Feature Ideas

This document catalogs potential features for future development. They are roughly grouped by theme. No commitment implied — these are ideas to explore.

---

## 🌐 Sharing & Publishing

### 1. Public Portfolio / Branded Timeline Page
Allow admins to create a public-facing profile page with custom branding (logo, colors, bio) that displays selected events. Useful for artists, photographers, or anyone who wants a visual timeline portfolio. Each user could have `/u/{username}`.

### 2. Embeddable Timeline Widget
A lightweight JS embed snippet that external sites can drop in to show a timeline of public events. Think GitHub profile README stats cards, but for life events.

### 3. RSS / Atom / JSON Feed
Generate a standard feed of public events (or per-collection). Enables integration with feed readers, IFTTT, and automated downstream publishing.

### 4. Social Image / Open Graph Cards
Auto-generate share cards (OG images) for events. When an event link is shared, show a nice card with title, date, location, and thumbnail.

---

## 🔐 Authentication & Access

### 5. OAuth / Social Login
Allow login via Google, GitHub, or other OAuth providers alongside the current password auth. Reduces friction for self-hosted family deployments.

### 6. WebAuthn / Passkeys
Passwordless authentication using hardware keys (YubiKey) or platform authenticators (Touch ID, Windows Hello). Higher security, no passwords to leak.

### 7. API Token Management
Let admins generate scoped API tokens (read-only, read-write, per-year) for external integrations. Useful for scripts, IFTTT, or mobile clients without using session cookies.

### 8. Granular Multi-User Permissions
Currently users exist but have equal access. Add roles: admin, editor (can manage events/people), viewer (read-only), and share-recipient (can only see shared content).

---

## 📅 Calendar & Scheduling

### 9. CalDAV Sync (Two-Way)
Sync events bidirectionally with external calendars (Google Calendar, iCloud, Outlook). Would let users manage events in their preferred calendar app and have them appear in TRACES.

### 10. Recurring Event Expansion
Currently recurring events are stored with a rule string but not automatically expanded. Generate actual instances on the fly and render them appropriately in timeline, calendar, and stats views.

### 11. Event Reminders / Notifications
Pop-up or email reminders for upcoming events. Could be configured per-event (e.g., "remind me 1 day before") or as a daily digest of "what's happening today."

### 12. Timeline Gantt / Bar Chart View
Visualize events on a horizontal bar chart spanning the year. Useful for seeing event density, gaps, and overlapping sequences at a glance.

---

## 🤖 AI & Automation (Ollama)

### 13. AI Auto-Description
Given a title and tags, ask Ollama to generate a rich description for an event. Fills in the description field on event creation with a single click.

### 14. AI Tag Suggestions
Analyze event title + description and suggest relevant tags. Could be shown as recommendations in the admin event form.

### 15. AI Event Summaries (Weekly / Monthly / Yearly)
Periodic AI-generated narratives: "This March you visited 3 countries, attended 2 concerts, and spent 12 days outdoors." Could be delivered via email or shown in the Wrapped view.

### 16. Smart Event Deduplication
Detect and merge duplicate events (same title + same date + same location) using fuzzy matching or Ollama embeddings.

### 17. AI Photo Captioning
For events with images but no description, pass the image to a vision model (via Ollama) and generate a caption.

---

## 📱 PWA & Mobile

### 18. Offline Support
Cache the app shell and recent events via Service Worker so the timeline is usable without internet. Queue writes for when connectivity returns.

### 19. Push Notifications
Browser push notifications for: new events created, memories triggered, uploads complete. Uses the existing Service Worker foundation.

### 20. Mobile-Optimized Photo Capture
Better mobile camera integration: swipeable gallery, pinch-to-zoom, full-screen media viewer with captions.

### 21. Share Target API
Register as a share target on mobile so users can "share" a photo from their camera roll directly into TRACES as a new event.

---

## 📊 Analytics & Insights

### 22. Advanced Dashboard Charts
Admin dashboard with: event creation trend line, media-type pie chart, location heatmap, person activity sparklines, cumulative event count over time.

### 23. Streek & Habit Tracking
Allow events to be optionally marked as "habit" type with target frequency. Show streak counters, compliance rate, and a habit-specific heatmap.

### 24. Geolocation Heatmap
Density heatmap overlay on the map view showing event clustering (not just individual markers). Helps visualize where most of your life happens.

### 25. "Time Machine" / Random Event
A button that jumps to a random past event. Nostalgia mode. Could be a screensaver-style slideshow of random media from the archive.

### 26. Life Calendar
A single-page view showing your entire recorded life as a dense matrix of days (rows = weeks, columns = weekdays). Each cell color-coded by event count or mood. Scrollable across years.

---

## 💾 Data & Integration

### 27. Webhook / Outgoing Hooks
POST JSON payloads to external URLs on event create/update/delete. Enables real-time integration with Discord, Slack, Home Assistant, Telegram, etc.

### 28. Full Archive Export
One-click download of the entire database + all media as a portable ZIP. For backups, migration, or peace of mind.

### 29. PhotoAlbum Enhancements
Collections are currently flat. Add: cover photo selection, auto-generated slideshow, drag-and-drop reordering, EXIF-based sorting.

### 30. Markdown Editor
Replace the plain `<textarea>` for descriptions with a proper Markdown editor (preview sidebar, toolbar for bold/italic/lists/links). Could in-browser only (no deps) or integrate a micro editor.

### 31. Location Reverse-Geo on Upload
When uploading a photo with EXIF GPS, auto-resolve the coordinates to a readable place name (via Nominatim) and pre-fill the location field.

---

## 🧩 Event Model Enhancements

### 32. Custom Fields / Metadata
Allow admins to define custom fields on events (e.g., "mood", "energy level", "cost", "rating", "weather"). Fields would be per-user or global, with type support (number, text, select, emoji).

### 33. Event Relationships
Link events together: "after/before", "part of series", "related". Visualize connected events in the timeline with connector lines or grouped rendering.

### 34. Event Drafts / Scheduled Publishing
Create events in draft state. Optionally schedule a publish date/time so they appear on the timeline at a future point.

### 35. Mood / Emotion Tracking
Add a mood emoji selector per event (😊 😢 😡 😴 🎉). Show mood distribution in stats and a color-coded mood strip in the timeline.

### 36. Event Check-Ins / Duration Tracking
If an event has a start and end time, show a duration badge. Could add a "check-in" flow on mobile to start/stop tracking an activity.

---

## 🛠 Quality of Life

### 37. Bulk Media Import
Upload a folder/zip of photos and have them auto-create events based on EXIF dates. Group photos taken within N hours into a single event.

### 38. CSV/JSON Import Mapping UI
When importing CSV, let the user map columns to fields (title, date, location, tags) via a drag-and-drop UI instead of hardcoded column order.

### 39. Notifications Hub
Instead of just Gotify, support multiple notification channels: email, webhook, Gotify, Pushover, Telegram, ntfy.sh — configurable per event type.

### 40. What's New / Changelog in Admin
Show the CHANGELOG.md in the admin panel after upgrades so admins know what changed.

### 41. Keyboard Shortcuts
Navigation shortcuts: `n` new event, `/` search, `t` toggle theme, `1-6` switch tabs, `Esc` close modals.

---

## 🏗 Infrastructure & DevOps

### 42. Read Replica / Remote Database Support
Support PostgreSQL or MySQL alongside SQLite for higher-scale deployments. Useful for multi-user family installations.

### 43. S3-Compatible Media Storage
Store uploaded media on S3/MinIO/B2 instead of local disk. Needed for horizontally scaled deployments.

### 44. Health Dashboard
Admin page showing: DB size, media disk usage, backup status, last sync times (Immich), memory config, and version info. Could replace /health with a richer view.

### 45. Rate Limiting
Protect auth endpoints and uploads from brute force / abuse. Configurable per-IP or per-session.

---

## 💡 "Vaporware" / Stretch Ideas

- **Timeline Video Trailer** — Auto-generate a video montage of the year using Remotion (already a skill available). Select top-N events, overlay text, background music.
- **Family Tree Integration** — Link persons into a family tree. Show relationships in event metadata.
- **Geofence Triggers** — Use the geolocation API to prompt "You're near [event location]. Did something happen here?" when the user visits a past event location.
- **Life Score / Stats** — "You've been alive for X days. Tracked Y% of them. Visited Z countries." A vanity dashboard.
- **Collaborative Timelines** — Invite others to add events to a shared timeline (wedding planning, group trip).
- **Audio Transcription** — If an event has audio media, transcribe it with Whisper (via Ollama) and store the text as description.

---

*Feel free to open issues or PRs for any of these ideas. This file is a living document — add to it as inspiration strikes!*
