## Why

E-ink displays are ideal for always-on timeline/photo frame displays — low power, paper-like readability, and perfect for a personal year-review dashboard that sits on a desk or wall. Adding an e-ink mode makes the TRACES timeline viewable on e-ink devices (Boox, Kindle, reMarkable) and as a static photo frame display.

## What Changes

- Add `?eink=1` URL parameter and/or cookie-activated e-ink mode that applies a high-contrast, no-animation CSS stylesheet
- Replace gradients, shadows, and backdrop filters with solid colors and flat design
- Set all interactive elements to minimum 48×48px touch targets
- Remove all CSS transitions, animations, and keyframes
- Replace color-only indicators (event type colors) with icon + label combinations
- Remove hover-dependent tooltips; show information inline or on tap
- Simplify the timeline/year layout — reduce marginal spacing, increase font sizes
- Add a high-contrast grayscale palette (pure black #000 on pure white #fff)
- Add toggle in the admin settings to enable e-ink mode persistently

## Capabilities

### New
- `eink-mode-stylesheet`: Alternative high-contrast, flat CSS for all pages
- `eink-mode-toggle`: URL-param and admin-settings toggle mechanism
- `eink-photo-frame`: Auto-advancing year/month view for photo frame use

### Modified
- *(none — no existing specs being modified)*

## Impact

- **Frontend**: New eink.css stylesheet; JS toggle handler; viewport meta adjustments for e-ink
- **Backend**: (minimal) Admin settings option to persist e-ink preference
- **Database**: Optional new column for e-ink preference if persisted
- **Dependencies**: None
