## ADDED Requirements

### Requirement: Upload response includes reverse-geocoded location suggestion
When an image upload yields EXIF GPS coordinates, the system SHALL resolve those coordinates to a human-readable place name using the Nominatim reverse-geocoding API and include it as `location_suggestion` in the upload response. The resolution SHALL be best-effort and non-blocking: upload success MUST NOT depend on geocoding success, and a request timeout of 2 seconds SHALL apply. The Nominatim endpoint SHALL be configurable via the `TRACES_NOMINATIM_URL` environment variable (default `https://nominatim.openstreetmap.org/reverse`). All requests SHALL include a descriptive `User-Agent` header identifying the application.

#### Scenario: Image with EXIF GPS resolves successfully
- **WHEN** an admin uploads an image whose EXIF contains GPS coordinates and Nominatim returns a place name
- **THEN** the upload response includes `location_suggestion` with the readable place name

#### Scenario: Image with EXIF GPS but geocoding fails
- **WHEN** an admin uploads an image with EXIF GPS but Nominatim is unreachable, times out, or returns an error
- **THEN** the upload response omits `location_suggestion` and the upload still succeeds

#### Scenario: Image without EXIF GPS
- **WHEN** an admin uploads an image with no GPS coordinates in EXIF
- **THEN** the upload response omits `location_suggestion`

### Requirement: Admin UI suggests the geocoded location
The admin media upload flow SHALL surface the `location_suggestion` to the user and allow pre-filling the event location field with it, requiring explicit user confirmation before it is persisted.

#### Scenario: Location suggestion shown after upload
- **WHEN** an upload response contains `location_suggestion`
- **THEN** the admin UI displays the suggestion and provides a control to fill the event location field with it

#### Scenario: User declines suggestion
- **WHEN** the user chooses not to accept the suggested location
- **THEN** the event location field remains unchanged and no suggested text is persisted
