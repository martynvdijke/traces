## 1. Video Multimedia Support

- [x] 1.1 Extend media type enum to include `video` in `internal/models/models.go`
- [x] 1.2 Update upload handler to accept video extensions (.mp4, .webm, .mov, .avi, .mkv, .flv, .wmv, .m4v, .3gp, .ogv) with `video/*` MIME validation
- [x] 1.3 Render `<video>` elements for video media in admin event form and public timeline
- [x] 1.4 Show video icon for video media type in media icon helper

## 2. Background Image Resizing Optimizations

- [x] 2.1 Apply `object-fit: cover` to background images on the timeline and admin pages

## 3. OpenStreetMap Location Support

- [x] 3.1 Add Leaflet map with OpenStreetMap tile layer for event location display
- [x] 3.2 Add Nominatim reverse geocoding for location lookup on click in admin event form
- [x] 3.3 Render OSM map for public timeline and admin event details

## 4. Verify

- [x] 4.1 Run `npx tsc` to verify TypeScript compilation
- [x] 4.2 Run `go build -o traces-server .` to verify Go compilation
- [x] 4.3 Run `go test ./...` to verify existing tests still pass
