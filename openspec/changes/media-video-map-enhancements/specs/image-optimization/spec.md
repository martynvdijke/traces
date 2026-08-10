## ADDED Requirements

### Requirement: Image uploads generate responsive size variants
The system SHALL generate multiple size variants for every newly uploaded raster image (excluding GIF, SVG, TIFF). Variants SHALL be: `_thumb` at 300px longest edge (existing contract), `_sm` at 640px longest edge, and `_md` at 1280px longest edge. The full-size file SHALL remain resized to a maximum of 1920px longest edge. The upload response SHALL include a `variants` object mapping variant names to their URLs.

#### Scenario: JPEG upload produces all variants
- **WHEN** an admin uploads a 4000x3000 JPEG
- **THEN** the upload response contains `variants` with `thumb`, `sm`, and `md` entries whose URLs resolve to image files at 300px, 640px, and 1280px longest edge respectively, and the full-size file is at most 1920px

#### Scenario: Small image upload skips upscaling
- **WHEN** an admin uploads an 800x600 image
- **THEN** no variant is larger than the source dimensions and all variant files exist without upscaling

#### Scenario: GIF upload
- **WHEN** an admin uploads an animated GIF
- **THEN** the original GIF is stored unchanged and no variants or thumbnail are generated

### Requirement: Image variants use a faster high-quality resizer
The system SHALL resize images using a high-quality resampling algorithm faster than the current `draw.CatmullRom` implementation (e.g., `nfnt/resize` Lanczos3). Resizing SHALL preserve the source aspect ratio and never upscale beyond source dimensions.

#### Scenario: Large image resized efficiently
- **WHEN** the resize pipeline processes a 10000x10000 image within the allowed dimension limit
- **THEN** resizing completes using the new algorithm and all variants maintain the original aspect ratio

### Requirement: Modern format re-encoding with quality tuning
The system SHALL re-encode the `_sm` and `_md` variants of JPEG and PNG uploads to lossy WebP (quality 80) when WebP encoding is available. The `_thumb` variant SHALL remain in the original format. JPEG quality for the full-size variant SHALL be configurable via the `TRACES_JPEG_QUALITY` environment variable (default 82). Re-encoded variants SHALL NOT contain EXIF metadata.

#### Scenario: JPEG upload yields WebP variants
- **WHEN** an admin uploads a JPEG and WebP encoding is available
- **THEN** the `sm` and `md` variant files are WebP-encoded, the `thumb` variant is JPEG, and the full-size file is JPEG at the configured quality

#### Scenario: WebP encoding unavailable
- **WHEN** an admin uploads a JPEG and WebP encoding fails or is unavailable
- **THEN** all variants are saved in the original format and the upload still succeeds

#### Scenario: EXIF stripped from variants
- **WHEN** an admin uploads a JPEG containing EXIF metadata
- **THEN** the generated `sm` and `md` variants contain no EXIF data

### Requirement: Frontend serves responsive images with srcset
The frontend SHALL render image media in timeline, grid, and detail views using `srcset`/`sizes` attributes referencing the available variants, so browsers select the appropriate size. The frontend SHALL use a `<picture>` element with a WebP source and a JPEG/PNG fallback `<img>` when variants are present.

#### Scenario: Browser supports WebP
- **WHEN** a WebP-capable browser renders an event image that has WebP variants
- **THEN** the browser loads the WebP `_md` or `_sm` variant appropriate to the viewport

#### Scenario: Browser does not support WebP
- **WHEN** a browser without WebP support renders the same event image
- **THEN** the browser loads the fallback original-format image

#### Scenario: Event without variants
- **WHEN** an event has only `media_url` and `thumbnail` (e.g., pre-existing or GIF media)
- **THEN** the frontend renders with the existing single-image behavior
