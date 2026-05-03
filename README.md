# TRACES - Your Year in Review

A personal timeline management system for capturing and preserving everyday moments, special events, and memories throughout the year.

## Overview

TRACES (Timeline, Reminders, And Curated Everyday Stories) is a self-hosted web application that helps you document and visualize life's journey. From daily moments to milestone celebrations, TRACES provides a beautiful timeline view, interactive map, and gallery to relive your memories.

## Features

- **Timeline View** - Chronological display of events with alternating left/right cards
- **Activity Graph** - GitHub-style contribution heatmap showing event density
- **Media Gallery** - Image, video, and audio player with modal viewing
- **Map View** - Interactive Leaflet/OpenStreetMap showing geo-tagged events
- **Persons** - Track people involved in events with avatars and color coding
- **Search & Filter** - Filter by year, month, tags, or search titles
- **Stats Dashboard** - Comprehensive analytics: events, locations, media, persons, years
- **Gotify Notifications** - Real-time alerts for new events, uploads, and activity
- **Admin Panel** - Full CRUD for events, persons, media upload, and settings
- **Dark Mode** - Automatic theme following system preference
- **Public Sharing** - Create shareable links for specific events or years
- **Import/Export** - Bulk import (JSON/CSV) and export (JSON/CSV)
- **Clone Events** - Duplicate events to new dates quickly

## Tech Stack

- **Backend**: Go 1.26 with Gin framework
- **Database**: SQLite3 (self-contained, no external DB required)
- **Frontend**: Vanilla JavaScript, Bootstrap 5, Leaflet maps
- **Maps**: OpenStreetMap via Leaflet
- **Notifications**: Gotify

## Quick Start

### Using Docker (Recommended)

```bash
docker build -t traces .
docker run -d -p 6270:6270 -v traces-data:/db traces
```

Then open http://localhost:6270 in your browser.

### Manual Setup

```bash
# Install dependencies
go mod download

# Build the server
go build -o traces-server .

# Run
./traces-server
```

The server will start on http://localhost:6270

## Environment Variables

| Variable | Description | Default |
|----------|-------------|----------|
| PORT | HTTP port | 6270 |
| DOCKER | Run in Docker mode | false |
| PUBLIC_MODE | Allow unauthenticated public events | false |
| GOTIFY_URL | Gotify server URL for notifications | - |
| GOTIFY_TOKEN | Gotify app token | - |
| GOTIFY_ENABLED | Enable Gotify notifications | false |

## Project Structure

```
traces/
├── main.go           # Go backend (Gin framework)
├── main_test.go     # Unit tests
├── go.mod           # Go module
├── Dockerfile       # Docker build
├── static/
│   ├── index.html   # Main timeline page
│   ├── admin.html   # Admin management panel
│   ├── map.html    # Standalone map page
│   ├── login.html  # Admin login
│   ├── setup.html  # First-time setup
│   ├── style.css   # Shared styles
│   └── favicon.svg # App icon
└── media/           # Uploaded media files
```

## Default Ports

- **HTTP**: 6270 (configurable via PORT env var)
- Docker EXPOSE: 6270

## API Endpoints

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /api/events | No | List events (filter by year/month) |
| GET | /api/events/full | No | All events with person data |
| GET | /api/events/search | No | Search events |
| POST | /api/events | Yes | Create/update event |
| DELETE | /api/events | Yes | Delete event |
| POST | /api/upload | Yes | Upload media file |
| GET | /api/stats | No | Event statistics |
| GET | /api/contributions | No | Activity heatmap data |
| GET | /api/map | No | GeoJSON for map |
| GET | /api/persons | No | List persons |
| POST | /api/persons | Yes | Create/update person |
| DELETE | /api/persons | Yes | Delete person |
| GET/POST | /api/gotify/config | Yes | Gotify settings |
| POST | /api/gotify/test | Yes | Test notification |
| POST | /api/login | No | Admin login |
| POST | /api/logout | No | Admin logout |

## First-Time Setup

1. Navigate to http://localhost:6270
2. You'll be redirected to /setup.html
3. Create your admin username and password
4. Login at /login.html to access the admin panel

## Tech Philosophy

TRACES is designed to be:
- **Self-contained** - No external dependencies beyond Go and SQLite
- **Simple** - Minimal configuration, batteries included
- **Personal** - Track your life, not just application metrics
- **Beautiful** - Clean timeline and gallery views

## License

MIT