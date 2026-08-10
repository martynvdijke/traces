# Design: Video thumbnails, image resizing optimizations, OSM location features

## Context

TRACES is a monolithic Go (Gin) server serving static HTML/TypeScript from `/static/`. Media uploads go through `handleUpload` (main.go:1173), which:

- Validates extension + MIME per `media_type` (image/video/audio)
- Stores files at `/media/<hash[:2]>/<hash><ext>`
- For images (excluding gif/svg/tiff): decodes with `golang.org/x/image`, resizes to max 1920px via `resizeImage` (pure-Go `draw.CatmullRom.Scale`), and writes a 300px `_thumb` variant
- Extracts EXIF GPS via `extractEXIFGPS` (goexif) and returns `latitude`/`longitude` in the upload response

Frontend: `static/app.ts` renders timeline/grid thumbnails using `e.thumbnail || e.media_url` — for videos there is **no thumbnail**, so full video files are downloaded as previews. The public map (`static/map.html` + `ts/map.ts`) uses Leaflet with a CartoDB (OSM data) tile layer, a `/api/map` GeoJSON endpoint, and no clustering. The admin event form has a Leaflet location picker (`ts/admin.ts`) and location autocomplete against existing event locations via `/api/autocomplete`.

## Goals / Non-Goals

**Goals:**
- Generate a poster-frame thumbnail for every new video upload and use it everywhere video previews render
- Replace the slow CatmullRom scaler with a faster high-quality pure-Go resizer
- Emit responsive size variants per image and re-encode with quality tuning (including WebP where beneficial), stripping EXIF from variants
- Serve variants via `srcset`/`sizes` so browsers fetch the right size
- Reverse-geocode EXIF GPS to a readable place name via Nominatim on upload
- Cluster map markers, add media thumbnails to popups, and support location filtering on the map

**Non-Goals:**
- AVIF encoding (pure-Go encoders are immature; WebP covers the modern-format goal)
- Video transcoding, audio processing, or streaming (poster extraction only)
- Generating variants for pre-existing media (additive; optional backfill is out of scope for this change)
- Bulk geocoding of historical events (upload-time only)
- Self-hosting tile servers or offline map tiles

## Decisions

### D1: Use `nfnt/resize` (Lanczos3) instead of `draw.CatmullRom`
- **Decision**: Swap `resizeImage` internals to `github.com/nfnt/resize` `Lanczos3` resampling.
- **Rationale**: `draw.CatmullRom` is a generic `draw.Scaler` and measurably slower for large images; `nfnt/resize` is pure Go (no CGO, keeps static single-binary builds), well-tested, and offers `Lanczos3` which yields sharper downsizes for photographic content.
- **Alternatives considered**: `bimg`/libvips (much faster but CGO — breaks the existing static/Docker build and adds native lib deps); `github.com/disintegration/imaging` (fine, but `nfnt/resize` is the minimal, dependency-light option).

### D2: Variant naming scheme `_thumb` / `_sm` / `_md`, full-size = original filename
- **Decision**: Keep existing `_thumb` (300px, backward compatible with all current `e.thumbnail` consumers) and add `_sm` (640px) and `_md` (1280px). The full variant is the resized-to-1920px original file (current behavior).
- **Rationale**: Preserves all existing thumbnail consumers with zero changes while adding progressive sizes for `srcset`. Names are derivable from the base hash filename — no DB schema change needed.
- **Alternative considered**: Storing variants in a media table — rejected; single-user timeline doesn't warrant a schema change yet.

### D3: WebP via `github.com/chai2010/webp`, JPEG quality configurable
- **Decision**: Encode `_sm` and `_md` variants to WebP (lossy, quality 80) when the source is a photographic format (jpeg/png); keep `_thumb` as the original format for maximum compatibility (favicons/cards). Add `TRACES_JPEG_QUALITY` env (default 82) for the full-size JPEG path. Variants are decoded-to-`image.Image` re-encodes, so EXIF is stripped automatically.
- **Rationale**: `chai2010/webp` is pure Go (no CGO). Re-encoding from the decoded image strips EXIF/GPS metadata from published variants — a privacy win for the public timeline, while the original upload keeps full quality and EXIF.
- **Alternatives**: AVIF (`go-avif` etc. immature); `golang.org/x/image/webp` is decode-only.
- **Risk**: WebP unsupported in very old browsers → `<picture>` element with `<img src="jpg">` fallback in frontend.

### D4: Video posters via optional `ffmpeg` binary
- **Decision**: After saving a video, run `ffmpeg -y -i <video> -ss 00:00:01 -vframes 1 -vf scale=640:-2 <hash>_thumb.jpg` if `ffmpeg` is on PATH. On success, set `thumbnail` to the poster URL (format matches existing image thumbnail contract). If `ffmpeg` is absent or fails, return `thumbnail: ""` and the frontend keeps the current generic video placeholder.
- **Rationale**: Go stdlib has no video decoding; `ffmpeg` is the standard, widely-available tool for frame extraction. Optional dependency keeps the app usable without it.
- **Alternatives**: `github.com/3d0c/gmf` (wraps ffmpeg libs via CGO — rejected); embedding ffmpeg statically (too heavy).

### D5: Nominatim reverse-geocoding with strict politeness
- **Decision**: After EXIF GPS extraction succeeds in `handleUpload`, call `GET https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=..&lon=..&zoom=16` with a `User-Agent: traces/<version>` header and a 2s timeout, **non-blocking** (goroutine or short-lived best-effort after response). Return the suggested `display_name` as `location_suggestion` in the upload response; the admin UI pre-fills the location field (user-confirmed, not auto-saved). Configurable `TRACES_NOMINATIM_URL` for self-hosted instances.
- **Rationale**: Matches the OpenStreetMap usage policy (1 req/sec, valid User-Agent). Best-effort keeps uploads fast; suggestion-only avoids storing wrong geocodes.
- **Risk**: Rate limiting/banning → mitigation: politeness headers, configurable endpoint, failure is silent (no location suggestion).

### D6: Map clustering + popup thumbnails via Leaflet.markercluster
- **Decision**: Add `leaflet.markercluster` (CDN) to `map.html`; wrap marker creation in `L.markerClusterGroup()`. Extend `/api/map` GeoJSON features with `thumbnail` (poster/thumb URL) and render `thumbnail ? <img> : <video|audio>` in popups. Add a location filter input that filters `mapEventsData` client-side by `location` text.
- **Rationale**: Clustering is the standard Leaflet plugin; popup thumbnails now work for both images (existing `media_url`) and videos (poster via new `thumbnail` field).

## Risks / Trade-offs

- **[New Go deps (nfnt/resize, chai2010/webp) add maintenance surface]** → Both are stable, long-lived, pure-Go projects; keep usage minimal and isolated in a `media.go` helper.
- **[ffmpeg missing → videos still lack posters]** → Explicit fallback path + documented install note; behavior degrades to today's state, never breaks.
- **[Nominatim rate limits / ban]** → User-Agent + timeout + configurable endpoint + silent failure (suggestion only).
- **[WebP browser support]** → `<picture>` with JPEG fallback; `_thumb` stays original format.
- **[Variant re-encode strips EXIF from variants]** → Accepted (privacy win); original file keeps EXIF for future use.
- **[Upload time increase from 2 extra encodes]** → Both variants are smaller than the 1920 full-size encode already performed; acceptable for a single-user app. Consider async variant generation if it becomes an issue (out of scope).

## Migration Plan

1. Add new Go dependencies; introduce `media.go` with resize/variant/quality helpers (no behavior change until wiring).
2. Update `handleUpload` to use new pipeline + video poster + reverse-geocode suggestion; keep response backward compatible (`url`, `thumbnail`, `media_type`, optional `latitude`/`longitude`; add `variants`, `location_suggestion`).
3. Frontend: swap thumbnail sources to poster/`_thumb`, add `srcset` for grid/detail images, `<picture>` WebP wrapper.
4. Map: add clustering + popup thumbnails + filter.
5. Rollback: revert frontend to `e.thumbnail || e.media_url`; backend changes are additive — old thumbnails continue to work.

## Open Questions

- Should `_sm`/`_md` be generated for PNG (non-photo) uploads too, or only JPEG? (Proposal: yes, size variants for all raster formats; WebP only for jpeg/png.)
- Exact poster capture time (`-ss 00:00:01` vs 10%) — tune during implementation with a sample video.
- Whether the admin upload form should auto-save the reverse-geocoded suggestion or always require confirmation (proposal: always confirm).
