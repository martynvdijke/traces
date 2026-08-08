## ADDED Requirements

### Requirement: Admin log viewer tab
The admin panel SHALL include a "Logs" tab in the top navigation, positioned after the Backup tab, with an <i class="fa-solid fa-list"></i> icon and "Logs" label.

#### Scenario: Logs tab appears in nav
- **WHEN** an admin loads the admin.html page
- **THEN** the navigation bar SHALL include a "Logs" tab button with a list icon

#### Scenario: Activating Logs tab
- **WHEN** an admin clicks the "Logs" tab
- **THEN** the log viewer pane SHALL display with current logs loaded

### Requirement: Log viewer displays structured log entries
The log viewer SHALL display log entries in a table with columns: Timestamp, Severity, Source, Message, Details (expandable metadata).

#### Scenario: Log table rendered
- **WHEN** the log viewer loads
- **THEN** it SHALL fetch log entries from `GET /api/logs`
- **THEN** entries SHALL display in a table sorted by timestamp descending (newest first)

#### Scenario: Expand metadata
- **WHEN** an admin clicks "Details" on a log entry
- **THEN** the metadata JSON SHALL expand inline in a code block

#### Scenario: Empty state
- **WHEN** no logs exist
- **THEN** the viewer SHALL show "No log entries found" message

### Requirement: Severity badge coloring
Each severity level SHALL display with a distinct color badge: debug (gray), info (blue), warn (yellow), error (red).

#### Scenario: Severity badges render correctly
- **WHEN** log entries are displayed
- **THEN** each entry SHALL show a colored badge matching its severity level

### Requirement: Filter by severity
The log viewer SHALL provide a dropdown to filter by severity level: All, Debug, Info, Warn, Error.

#### Scenario: Filter by severity
- **WHEN** an admin selects "Error" from the severity filter
- **THEN** only log entries with severity "error" SHALL be displayed

### Requirement: Filter by source
The log viewer SHALL provide a dropdown to filter by log source (system, gotify, email, ollama, immich, memories, backup, etc.).

#### Scenario: Filter by source
- **WHEN** an admin selects "gotify" from the source filter
- **THEN** only log entries from the "gotify" source SHALL be displayed

### Requirement: Search logs
The log viewer SHALL provide a text search input that filters logs by message content.

#### Scenario: Search by message text
- **WHEN** an admin types "saved" in the search box
- **THEN** only log entries whose message contains "saved" SHALL be displayed

### Requirement: Auto-refresh
The log viewer SHALL auto-refresh every 10 seconds while the Logs tab is active.

#### Scenario: Auto-refresh fetches new entries
- **WHEN** the Logs tab is visible
- **THEN** the log list SHALL refresh every 10 seconds via `GET /api/logs`

#### Scenario: Auto-refresh stops when tab hidden
- **WHEN** the admin switches to a different tab
- **THEN** the auto-refresh interval SHALL be cleared

### Requirement: Pagination
The log viewer SHALL paginate results, showing 50 entries per page with "Previous" and "Next" buttons.

#### Scenario: Navigate pages
- **WHEN** there are more than 50 log entries
- **THEN** the viewer SHALL show "Next" button to load older entries
- **WHEN** on page 2 or later
- **THEN** the viewer SHALL show "Previous" button

### Requirement: Log severity verbosity control
The admin panel SHALL include a verbosity control in the Logs tab header that sets the minimum severity level stored in the database. Default: `warn`.

#### Scenario: View current verbosity
- **WHEN** the Logs tab loads
- **THEN** it SHALL fetch the current verbosity setting from `GET /api/logs/settings`
- **THEN** the verbosity selector SHALL reflect the current setting

#### Scenario: Change verbosity
- **WHEN** an admin selects a new severity level from the verbosity dropdown
- **THEN** a `POST /api/logs/settings` request SHALL save the new minimum severity
- **THEN** only entries at or above the selected severity SHALL be shown going forward

### Requirement: Clear logs button
The log viewer SHALL include a "Clear Logs" button to delete all log entries.

#### Scenario: Clear all logs
- **WHEN** an admin clicks "Clear Logs" and confirms
- **THEN** a `DELETE /api/logs` request SHALL delete all log entries
- **THEN** the log viewer SHALL show the empty state

### Requirement: Log viewer responsive layout
The log viewer SHALL be responsive and match the admin panel's existing design system (cards, grid, mobile breakpoints).

#### Scenario: Mobile layout
- **WHEN** viewed on a screen under 768px
- **THEN** filters SHALL stack vertically
- **THEN** the log table SHALL be horizontally scrollable
