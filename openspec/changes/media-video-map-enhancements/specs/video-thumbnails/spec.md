## ADDED Requirements

### Requirement: Video uploads produce a poster-frame thumbnail
The system SHALL generate a poster-frame thumbnail for every newly uploaded video file when an `ffmpeg` binary is available on the server PATH. The poster SHALL be extracted from within the video content, sized to a maximum width of 640 pixels, and stored as a JPEG file named `<hash>_thumb.jpg` in the same media subdirectory as the video. The `thumbnail` field of the upload response SHALL contain the poster URL when generation succeeds.

#### Scenario: Video uploaded with ffmpeg available
- **WHEN** an admin uploads a `.mp4` video and `ffmpeg` is installed on the server
- **THEN** the upload response includes a non-empty `thumbnail` URL pointing to a JPEG poster frame derived from the video

#### Scenario: Video uploaded without ffmpeg
- **WHEN** an admin uploads a video and `ffmpeg` is not installed or the extraction command fails
- **THEN** the upload response includes an empty `thumbnail` field and the video is still stored and served normally

### Requirement: Timeline and grid views preview videos with thumbnails
The frontend SHALL use the video's poster thumbnail (from the `thumbnail` field) as the preview in timeline, grid, and lightbox list views instead of loading the full video file. When no thumbnail exists, the frontend SHALL fall back to the existing muted `<video>` placeholder behavior.

#### Scenario: Video with poster rendered in timeline
- **WHEN** the timeline renders an event whose `media_type` is `video` and a non-empty `thumbnail` is present
- **THEN** the preview shows an `<img>` with the poster thumbnail rather than a `<video>` element

#### Scenario: Video without poster rendered in timeline
- **WHEN** the timeline renders an event whose `media_type` is `video` and `thumbnail` is empty
- **THEN** the preview renders the existing muted `<video>` placeholder
