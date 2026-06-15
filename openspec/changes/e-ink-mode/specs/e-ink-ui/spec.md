# e-ink-ui Specification

## Purpose
The e-ink mode adapts the TRACES interface for e-ink displays (Boox, Kindle, reMarkable) and static photo-frame use. It prioritizes readability, high contrast, and large touch targets while removing all motion, gradients, and hover-dependent interactions.

## Requirements

### Requirement: E-ink mode toggle
The application SHALL provide a mechanism to enable e-ink mode via URL parameter (`?eink=1`) and optionally persist the preference via cookie or admin setting.

#### Scenario: Enable via URL parameter
- **WHEN** a user appends `?eink=1` to any page URL
- **THEN** the page SHALL render in e-ink mode for that session
- **AND** a cookie SHALL be set to persist the preference for subsequent page loads

#### Scenario: Enable via admin settings
- **WHEN** an admin toggles "E-ink mode" in admin settings and saves
- **THEN** e-ink mode SHALL be enabled for all users site-wide
- **AND** the preference SHALL persist across server restarts

### Requirement: High contrast palette
All pages in e-ink mode SHALL use a strict black-on-white color scheme with no intermediate grays for text and backgrounds.

#### Scenario: Text and background colors
- **WHEN** e-ink mode is active
- **THEN** all text SHALL be `#000000` on `#ffffff` background
- **THEN** secondary text SHALL use `#333333` on `#ffffff`
- **THEN** borders and dividers SHALL use `#cccccc` solid lines
- **THEN** no element SHALL use `background-image` for content-bearing purposes

#### Scenario: Color is never sole carrier
- **WHEN** e-ink mode is active
- **THEN** any status/type indicator that uses color SHALL also include an icon, label, or pattern
- **THEN** colored badges SHALL have a visible border and label text

### Requirement: No motion or transitions
E-ink mode SHALL eliminate all CSS animations, transitions, and JavaScript-driven motion.

#### Scenario: Animations removed
- **WHEN** e-ink mode is active
- **THEN** all `transition`, `animation`, `@keyframes` CSS SHALL be disabled
- **THEN** all `transform` and `opacity` transitions SHALL be disabled
- **THEN** all JavaScript `requestAnimationFrame` loops SHALL NOT execute

### Requirement: Large touch targets
All interactive elements SHALL meet minimum size requirements for e-ink touch screens.

#### Scenario: Touch target sizing
- **WHEN** e-ink mode is active
- **THEN** all buttons, links, and interactive elements SHALL be minimum 48×48px
- **THEN** form inputs, selects, and checkboxes SHALL be minimum 44px height
- **THEN** touch targets SHALL have minimum 8px spacing between them

### Requirement: No CSS effects
Gradients, shadows, backdrop filters, and blur effects SHALL be removed in e-ink mode.

#### Scenario: Visual effects disabled
- **WHEN** e-ink mode is active
- **THEN** no element SHALL use `background: linear-gradient()` or `background: radial-gradient()`
- **THEN** no element SHALL use `box-shadow` or `text-shadow`
- **THEN** no element SHALL use `backdrop-filter` or `filter: blur()`
- **THEN** no element SHALL use `background: rgba()` or transparency effects

### Requirement: Simplified timeline layout
The timeline/year views SHALL reduce visual density and increase readability.

#### Scenario: Timeline view
- **WHEN** viewing the timeline in e-ink mode
- **THEN** event cards SHALL use solid borders instead of shadows for depth
- **THEN** the layout SHALL reduce side margins and increase content width
- **THEN** body text SHALL be minimum 18px
- **THEN** month labels SHALL be minimum 24px bold

### Requirement: Calendar/map views
Calendar and photo map views SHALL be simplified for e-ink readability.

#### Scenario: Calendar view
- **WHEN** viewing the calendar in e-ink mode
- **THEN** day cells SHALL show event dots as solid squares rather than colored circles
- **THEN** event month view SHALL show event titles in full (no truncation)
- **THEN** navigation arrows SHALL be minimum 48×48px

#### Scenario: Map view
- **WHEN** viewing the maps page in e-ink mode
- **THEN** the map SHALL use a light, high-contrast tile style if configurable
- **THEN** all map markers SHALL have visible borders and labels
