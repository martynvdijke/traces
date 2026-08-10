# Tasks: Video thumbnails, image resizing optimizations, OSM location features

## 1. Resize Engine & Variant Pipeline (backend)

- [x] 1.1 Add `github.com/nfnt/resize` and `github.com/chai2010/webp` to `go.mod`
- [x] 1.2 Create `media.go` helper module: variant size constants (300/640/1280/1920), `resizeImage` switched to Lanczos3 (`nfnt/resize`), no-upscale guard
- [x] 1.3 Add WebP encoder helper for JPEG/PNG sources (quality 80) with graceful fallback to original format
- [x] 1.4 Add JPEG quality configuration via `TRACES_JPEG_QUALITY` env (default 82)
- [x] 1.5 Rework `handleUpload` image path to emit `_thumb` (300), `_sm` (640, WebP), `_md` (1280, WebP), and full-size (≤1920) variants with the existing hash-based naming; keep `thumbnail` response field
- [x] 1.6 Add `variants` object to upload response (`thumb`, `sm`, `md` URLs) while keeping `url`/`media_type`/`thumbnail`/`latitude`/`longitude` backward compatible
- [x] 1.7 Verify GIF/SVG/TIFF uploads skip the pipeline unchanged (existing exclusion list preserved)

## 2. Video Poster Thumbnails (backend + frontend)

- [x] 2.1 Add `ffmpeg` poster extraction helper: `ffmpeg -y -i <video> -ss 00:00:01 -vframes 1 -vf scale=640:-2 <hash>_thumb.jpg`, only when `ffmpeg` is on PATH
- [x] 2.2 Wire poster extraction into `handleUpload` video path; set `thumbnail` to poster URL on success, empty string on absence/failure
- [x] 2.3 Update `static/app.ts` timeline/grid/lightbox previews: videos with a thumbnail render an `<img>` poster; without one keep the muted `<video>` placeholder
- [x] 2.4 Add a Go unit test for poster filename generation and the empty-thumbnail fallback contract

## 3. Responsive Images Frontend

- [x] 3.1 Update image rendering in `static/app.ts` to use `srcset`/`sizes` referencing `variants` (sm/md) with `media_url` as final fallback
- [x] 3.2 Wrap WebP-capable image rendering in a `<picture>` element with original-format `<img>` fallback
- [x] 3.3 Ensure events without `variants` (pre-existing, GIF, Immich-imported) render with existing single-image behavior
- [x] 3.4 Verify timeline, grid, lightbox, and map popup image paths all use the responsive pipeline

## 4. Reverse-Geocoding (backend + admin UI)

- [x] 4.1 Add Nominatim reverse-geocode client: `TRACES_NOMINATIM_URL` env (default `https://nominatim.openstreetmap.org/reverse`), `User-Agent` header, 2s timeout, `format=jsonv2&zoom=16`
- [x] 4.2 Call client after EXIF GPS extraction in `handleUpload` (best-effort, non-blocking); include `location_suggestion` in response on success
- [x] 4.3 In `ts/admin.ts`, surface `location_suggestion` in the upload flow with a confirm-to-fill control for the event location field
- [x] 4.4 Add Go unit test for reverse-geocode URL construction and silent-failure path

## 5. Map Enhancements (frontend + API)

- [x] 5.1 Extend `/api/map` GeoJSON features with `thumbnail` (poster/thumb URL) in `getMapData`
- [x] 5.2 Add `leaflet.markercluster` CDN to `static/map.html`; wrap markers in a `markerClusterGroup` in `ts/map.ts` with count badges
- [x] 5.3 Update popup rendering in `ts/map.ts` to show image/video poster thumbnails when available
- [x] 5.4 Add location filter control to the map view that client-side filters markers and the event list
- [x] 5.5 Verify zoom-out clustering, zoom-in expansion, and filter clear behavior

## 6. Verification & Docs

- [x] 6.1 Add/update Go unit tests for the resize pipeline, variant naming, and EXIF stripping in variants
- [x] 6.2 Run `task prepush` (fmt, vet, tsc, build, go test) and fix failures
- [ ] 6.3 Add Playwright e2e coverage for video upload poster + location suggestion flow (as feasible in `tests/`)
- [x] 6.4 Update README/docs: optional `ffmpeg` dependency and new `TRACES_JPEG_QUALITY`/`TRACES_NOMINATIM_URL` env vars
