## ADDED Requirements

### Requirement: Map markers cluster at low zoom levels
The public map view SHALL cluster event markers that are spatially close at low zoom levels and expand them into individual markers as the user zooms in. Clustering SHALL use the Leaflet.markercluster plugin. Marker clusters SHALL display a count badge of contained events.

#### Scenario: Dense area clusters at low zoom
- **WHEN** a map area contains many events with locations and the viewport is zoomed out
- **THEN** nearby markers are rendered as a cluster marker showing the count of contained events

#### Scenario: Zoom reveals individual markers
- **WHEN** the user zooms in on a cluster
- **THEN** the cluster expands and individual event markers become visible

### Requirement: Map popups show media thumbnails
The map view SHALL include the event's thumbnail URL in the GeoJSON data served by `/api/map`, and popups SHALL render an image thumbnail for image and video events when a thumbnail is available.

#### Scenario: Image event popup shows thumbnail
- **WHEN** a user opens the popup for an event whose `media_type` is `image`
- **THEN** the popup displays an image thumbnail using the event thumbnail or media URL

#### Scenario: Video event popup shows poster
- **WHEN** a user opens the popup for an event whose `media_type` is `video` and a thumbnail exists
- **THEN** the popup displays the video poster thumbnail image instead of an inline video player

### Requirement: Map view supports location filtering
The map view SHALL provide a location filter control that restricts displayed markers and the event list to events whose `location` matches the filter text.

#### Scenario: Filtering by location text
- **WHEN** the user enters a location query in the map view filter control
- **THEN** only events whose location contains the query are shown as markers and in the event list

#### Scenario: Clearing the location filter
- **WHEN** the user clears the location filter
- **THEN** all located events are displayed again
