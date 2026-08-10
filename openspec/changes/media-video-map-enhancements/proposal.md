# Proposal: Video thumbnails, image resizing optimizations, OSM location features

## Why

TRACES already accepts video, image, and audio uploads, but media handling has three gaps: (1) videos have no thumbnail/poster frames, so the timeline and grid views download full video files just to show a preview; (2) image resizing uses a slow pure-Go CatmullRom scaler and emits a single 1920px/300px pair in the original format, wasting bandwidth on high-DPI screens and large originals; (3) although uploads already extract EXIF GPS coordinates, the app never resolves them into a human-readable place name, and the map view lacks clustering for dense event areas.

## What Changes

- Generate a poster-frame thumbnail for every uploaded video (via `ffmpeg` when available, with graceful fallback to a generic video placeholder), and use it in timeline/grid/lightbox previews instead of loading the full video file
- Replace the pure-Go CatmullRom `resizeImage` with a faster high-quality resizer (`nfnt/resize` Lanczos) and generate multiple responsive size variants (thumb / small / medium / full) per image
- Re-encode images to modern formats with quality tuning: WebP output when beneficial (via a pure-Go encoder), configurable JPEG quality, and EXIF metadata stripping from resized variants
- Serve responsive image variants with `srcset`/`sizes` in the frontend so browsers pick the right size
- Add reverse-geocoding: after upload, resolve extracted EXIF GPS coordinates to a readable place name via Nominatim and prefill/suggest the event location field
- Enhance the map view: cluster markers for dense areas, show video/image thumbnails in popups, and add location-based filtering

## Capabilities

### New Capabilities
- `video-thumbnails`: Poster-frame generation for video uploads and thumbnail-based video previews across timeline, grid, map, and lightbox views
- `image-optimization`: Responsive size variants, modern-format re-encoding with quality tuning, and EXIF stripping for uploaded images
- `reverse-geocoding`: Resolve EXIF GPS coordinates to readable place names via Nominatim on upload
- `map-enhancements`: Marker clustering, media thumbnails in popups, and location filtering on the public map view

### Modified Capabilities

None — no existing specs to modify.

## Impact

- **Backend**: `handleUpload` (video poster extraction + resize pipeline), new `resizeImage`/`saveImage` rework, new variants naming (`_thumb`, `_sm`, `_md`, full), new `ffmpeg` invocation helper, new `reverseGeocode()` Nominatim client, configurable quality settings
- **Frontend**: `static/app.ts` (srcset/sizes attributes, video preview uses poster thumbnail, `<picture>`/WebP fallback), `ts/admin.ts` (location prefill on upload result), `ts/map.ts` (Leaflet marker clusters, popup thumbnails), `static/index.html`, `static/map.html`
- **Dependencies**: `github.com/nfnt/resize` (or similar pure-Go resizer), optional WebP encoder; `ffmpeg` becomes an optional runtime dependency for video posters (graceful fallback if absent)
- **Config**: Optional env vars for image quality / max dimensions (e.g. `TRACES_JPEG_QUALITY`)
- **API**: `POST /upload` response may include new `variants` object; no breaking endpoint changes
- **Tests**: Go unit tests for resize pipeline and variant naming; Playwright e2e coverage for video upload with poster and map clustering (as feasible)
