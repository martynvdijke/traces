<p align="center">
  <img src="static/logo.svg" alt="TRACES Logo" width="80" height="80">
</p>

<h1 align="center">TRACES</h1>
<p align="center">
  <em>Your Year in Review — Timeline, Memories &amp; Analytics</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go" alt="Go">
  <img src="https://img.shields.io/badge/TypeScript-6.0-3178C6?style=flat&logo=typescript" alt="TypeScript">
  <img src="https://img.shields.io/badge/SQLite3-003B57?style=flat&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/license-MIT-blue" alt="License">
  <img src="https://img.shields.io/badge/docker-ready-2496ED?style=flat&logo=docker" alt="Docker">
</p>

---

## Features

<table>
<tr>
<td width="50%">

**📅 Timeline** — Alternating event cards with rich media, markdown descriptions, and weather data.

**📊 Activity Graph** — GitHub-style contribution heatmap showing event density throughout the year.

**🖼️ Media Gallery** — Image, video, and audio player with modal viewing and infinite scroll.

**🗺️ Map View** — Interactive Leaflet map with geo-tagged events, colored markers by media type.

</td>
<td width="50%">

**👥 Persons** — Track people with avatars, color coding, bios, and linked events.

**📈 Year Comparison** — Side-by-side stats comparing any two years.

**🔔 Gotify Notifications** — Real-time alerts for new events, uploads, and activity.

**🌙 Dark Mode** — Automatic theme following system preference, with manual toggle.

</td>
</tr>
</table>

## Tech Stack

| Layer | Technology |
|-------|-----------|
| **Backend** | Go 1.26 with Gin framework |
| **Database** | SQLite3 via mattn/go-sqlite3 |
| **Frontend** | TypeScript 6.0, compiled to vanilla JS |
| **UI** | Bootstrap 5, Font Awesome 7 |
| **Maps** | Leaflet.js with OpenStreetMap |
| **Auth** | bcrypt password hashing, CSRF tokens |
| **CI/CD** | GitHub Actions, Playwright E2E tests |

## Quick Start

### Docker (Recommended)

```bash
docker build -t traces .
docker run -d -p 6270:6270 -v traces-data:/db traces
```

Open **[http://localhost:6270](http://localhost:6270)** in your browser.

### Manual Setup

```bash
# Install dependencies
go mod download
npm install

# Build TypeScript frontend
npm run build:ts

# Build the server
go build -o traces-server .

# Run
./traces-server
```

### Development

```bash
# Install dev tools (requires `air` for hot reload)
task dev

# Run Go unit tests
go test -v ./...

# Run E2E tests (starts server automatically)
task test-e2e

# Run all pre-push checks
task prepush
```

> See [AGENTS.md](AGENTS.md) for the full development guide and available tasks.

## First-Time Setup

1. Navigate to `http://localhost:6270`
2. You'll be redirected to `/setup.html`
3. Create your admin username and password
4. Login at `/login.html` to access the admin panel

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP port | `6270` |
| `DOCKER` | Run in Docker mode | `false` |
| `PUBLIC_MODE` | Allow unauthenticated access to public events | `false` |
| `GOTIFY_URL` | Gotify server URL for notifications | — |
| `GOTIFY_TOKEN` | Gotify app token | — |
| `GOTIFY_ENABLED` | Enable Gotify notifications | `false` |

## Project Structure

```
traces/
├── main.go                # Go backend (Gin framework)
├── main_test.go           # Go unit tests
├── go.mod / go.sum        # Go module dependencies
├── ts/                    # TypeScript source files
│   ├── index.ts           # Timeline page
│   ├── admin.ts           # Admin panel
│   ├── login.ts           # Login page
│   ├── setup.ts           # Setup page
│   └── map.ts             # Map page
├── static/                # Static assets served by the app
│   ├── index.html         # Main timeline page
│   ├── admin.html         # Admin management panel
│   ├── map.html           # Standalone map page
│   ├── login.html         # Admin login
│   ├── setup.html         # First-time setup
│   ├── style.css          # Shared styles
│   ├── js/                # Compiled JavaScript (gitignored)
│   │   ├── index.js       # Compiled from ts/index.ts
│   │   ├── admin.js       # Compiled from ts/admin.ts
│   │   └── ...
│   └── images/            # Static images
├── tests/                 # Playwright E2E tests
│   ├── index.spec.ts
│   ├── admin.spec.ts
│   └── features.spec.ts
├── playwright.config.ts   # Playwright configuration
├── tsconfig.json          # TypeScript configuration
├── Taskfile.yml           # Task runner
├── Dockerfile             # Docker build
└── media/                 # Uploaded media files
```

## API Endpoints

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `GET` | `/api/events` | No | List events (filter by year/month) |
| `GET` | `/api/events/full` | No | All events with person data |
| `GET` | `/api/events/search` | No | Search events |
| `POST` | `/api/events` | Yes | Create/update event |
| `DELETE` | `/api/events` | Yes | Delete event |
| `POST` | `/api/upload` | Yes | Upload media file |
| `GET` | `/api/stats` | No | Event statistics |
| `GET` | `/api/contributions` | No | Activity heatmap data |
| `GET` | `/api/map` | No | GeoJSON for map |
| `GET` | `/api/persons` | No | List persons |
| `POST` | `/api/persons` | Yes | Create/update person |
| `DELETE` | `/api/persons` | Yes | Delete person |
| `GET`/`POST` | `/api/gotify/config` | Yes | Gotify settings |
| `POST` | `/api/gotify/test` | Yes | Test notification |
| `POST` | `/api/login` | No | Admin login |
| `POST` | `/api/logout` | No | Admin logout |

## Philosophy

TRACES is designed to be:

- **Self-contained** — No external dependencies beyond Go and SQLite
- **Simple** — Minimal configuration, batteries included
- **Personal** — Track your life, not just application metrics
- **Beautiful** — Clean timeline, gallery, and map views

## License

MIT
