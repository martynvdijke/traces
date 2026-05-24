# Life Calendar & Public Profile — Design Spec

**Date:** 2026-05-24
**Status:** Draft
**Features:**
- Life Calendar (#26): Dense matrix view of all recorded life days
- Public Profile (#1): Public-facing timeline page at `/u/{username}`

---

## 1. Life Calendar

### 1.1 Overview

A new tab in the main timeline view showing the user's entire recorded history as a color-coded dense matrix. Rows = weeks, columns = weekdays (Mon–Sun). Each cell represents one day, colored by event count.

### 1.2 API Endpoint

**`GET /api/calendar/life?year=YYYY`** (auth-protected)

Returns a map of date strings to event counts, plus metadata:

```json
{
  "2020-01-01": 3,
  "2020-01-02": 0,
  "counts": { "2020-01-01": 3, ... },
  "min_year": 2020,
  "max_year": 2026
}
```

- Queries `timeline_events WHERE deleted_at IS NULL AND strftime('%Y', event_date) = ?`
- Groups by `event_date` with `COUNT(*)`
- Keys with zero events are omitted (the frontend can fill gaps via date iteration)
- Also returns `min_year`/`max_year` from the full event set (for year selector range)

### 1.3 Data Flow

```
[Tab click] → fetch /api/calendar/life?year=2026
           → parse date→count map
           → iterate day-by-day through the year
           → compute week row index and weekday column
           → create DOM grid cells with color levels
           → attach tooltip (date + count) on hover
[Year prev/next] → re-fetch with new year
[All Time toggle] → fetch all years in range, stack vertically
```

### 1.4 Frontend

**HTML:** Add a 7th tab to `static/index.html`:
- Nav: `<button class="nav-link" id="life-tab">Life</button>`
- Pane: `<div class="tab-pane fade" id="life-pane">`

Inside the pane:
- Year navigation row (prev/next buttons, year display, "All Time" toggle)
- Month labels row
- Grid container (53 rows × 7 cols)
- Stats footer row (total events, active days, longest streak)

**Color levels** (same as contribution graph):
| Level | Color (light) | Color (dark) |
|-------|---------------|--------------|
| 0     | `#ebedf0`     | `#161b22`    |
| 1     | `#9be9a8`     | `#0e4429`    |
| 2     | `#40c463`     | `#006d32`    |
| 3     | `#30a14e`     | `#26a641`    |
| 4     | `#216e39`     | `#39d353`    |

**All-Time mode:**
- Fetches data for all years from `min_year` to `max_year`
- Renders each year as a separate grid block stacked vertically
- Each block has a year label on the left

### 1.5 Implementation Order

1. Go tests for `getLifeCalendar`
2. Go handler + route registration
3. Frontend tab HTML
4. Frontend JS rendering (year + all-time modes)
5. CSS styles

### 1.6 Testing

- Empty DB → all cells level 0
- Single event on known date → cell for that date shows level 1
- Multiple events same date → level 4 (or appropriate)
- Soft-deleted events are excluded
- Events across multiple years
- All-time mode returns correct range

---

## 2. Public Profile

### 2.1 Overview

A public-facing page at `/u/{username}` displaying a user's public events with custom branding. No authentication required.

### 2.2 Route

**`GET /u/:username`** (no auth — public route)

### 2.3 Data Changes

**Schema migration (version 18):**
```sql
ALTER TABLE users ADD COLUMN bio TEXT DEFAULT '';
```

**Struct update** (`User`, `UserRow`):
- Add `Bio string` field (with JSON and template tags)

### 2.4 Handler Logic

1. Query user by username from `users` table
2. If not found → 404 JSON
3. Fetch events: `SELECT ... FROM timeline_events e LEFT JOIN persons p ... WHERE e.user_id = ? AND e.is_public = 1 AND (e.deleted_at IS NULL OR e.deleted_at = '') ORDER BY event_date ASC`
4. Compute stats: total events, date range (min/max date), distinct locations count
5. Render template with user info + events + stats

### 2.5 Template: `public-profile`

Embedded Go HTML template within the existing `htmxTemplateSource` or a new template set.

**Structure:**
- `<head>` with user's color as theme-color meta, custom OG tags
- Hero: Avatar (or fallback initial), display name, bio, color accent bar
- Event timeline: same `timeline-item` format as index.html (without edit/fav controls)
  - Date, title, location, tags, description (Markdown rendered)
  - Media thumbnails → lightbox modal
  - Person badges
- Stats bar: "127 events · 2018–2026 · 43 locations"
- Footer: "Powered by TRACES"

### 2.6 Admin Profile Settings

In the existing admin Users tab:
- Add bio field to user edit form
- Save handler already persists other fields — add bio

### 2.7 Implementation Order

1. Schema migration (add bio column)
2. Go tests for `getPublicProfile`
3. Go handler + route registration
4. Go template for profile page
5. Admin form update (bio field)
6. CSS styles (profile page, hero, stat bar)

### 2.8 Testing

- Unknown username → 404
- Known user with 0 public events → empty timeline
- Known user with public events → events rendered in HTML
- Deleted events excluded
- User exists but no public events → "No public events" message
- Template renders valid HTML with expected elements

---

## 3. Shared Patterns

**Both features** follow existing codebase patterns:
- Gin handlers returning JSON or rendered HTML templates
- SQLite queries with `deleted_at` filtering
- Go templates with the existing `FuncMap` (formatDate, renderMarkdown, etc.)
- Tests in `main_test.go` using `gin.TestMode`

## 4. Spec Self-Review

- ✅ No TBDs or TODOs
- ✅ Internal consistency: Life Calendar API follows contributions pattern; Public Profile follows route+template pattern
- ✅ Scoped: Two independent features, each decomposable into handler+route+template+test
- ✅ No ambiguity: Color levels match existing contribution graph; profile route path is explicit
