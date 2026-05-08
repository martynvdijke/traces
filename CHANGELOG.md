# [1.11.0](https://github.com/martynvdijke/traces/compare/v1.10.0...v1.11.0) (2026-05-08)


### Features

* show media thumbnails on timeline cards ([a0cd742](https://github.com/martynvdijke/traces/commit/a0cd74285c54b28596da33539b3553d52e8fc753))

# [1.10.0](https://github.com/martynvdijke/traces/compare/v1.9.0...v1.10.0) (2026-05-07)


### Bug Fixes

* initialize stats distribution arrays to prevent null JSON ([04ea3f8](https://github.com/martynvdijke/traces/commit/04ea3f827073a5d4c22013a55fdfc866b211ae8d))
* skip FTS5-dependent tests when FTS5 module is not available ([6abe21b](https://github.com/martynvdijke/traces/commit/6abe21bd9583792510360d42be564169cc9fad27))


### Features

* add full-text search, global search, advanced filters, and stats distributions ([a9af442](https://github.com/martynvdijke/traces/commit/a9af4427b27130b874d0c874f9110d1bce08b61f))
* add Immich memory import from immich.vandijke.xyz ([c288408](https://github.com/martynvdijke/traces/commit/c288408ca65c75b7933ecb5a4a56b312ee46f400))

# [1.9.0](https://github.com/martynvdijke/traces/compare/v1.8.13...v1.9.0) (2026-05-06)


### Features

* auto-generate Swagger docs with swaggo/gin-swagger ([d9c590d](https://github.com/martynvdijke/traces/commit/d9c590dc1681650d4ba917bdc8bde5c67356cc3e))

## [1.8.13](https://github.com/martynvdijke/traces/compare/v1.8.12...v1.8.13) (2026-05-06)


### Bug Fixes

* event creation bugs and add tags tab, improve e2e coverage ([aa177c1](https://github.com/martynvdijke/traces/commit/aa177c1e422fb6bcfc5f43c31758258782eb5408)), closes [#40](https://github.com/martynvdijke/traces/issues/40)

## [1.8.12](https://github.com/martynvdijke/traces/compare/v1.8.11...v1.8.12) (2026-05-06)


### Bug Fixes

* trigger ci ([eb0a020](https://github.com/martynvdijke/traces/commit/eb0a0201d8bd5d9e21ebc1e5a7db7c4b6326127c))

## [1.8.11](https://github.com/martynvdijke/traces/compare/v1.8.10...v1.8.11) (2026-05-06)


### Bug Fixes

* add in same taskfile ([96570a2](https://github.com/martynvdijke/traces/commit/96570a231662cef99b8d6aaab69155afbca87a9f))
 * handle NULL text columns in row scans to prevent silently dropped rows ([f55ece8](https://github.com/martynvdijke/traces/commit/f55ece8a80886b927d850d3d7c9269f8f392d4a7))

## [1.8.10](https://github.com/martynvdijke/traces/compare/v1.8.9...v1.8.10) (2026-05-06)


### Bug Fixes

 * include thumbnail column in saveEvent INSERT and UPDATE ([4d6d041](https://github.com/martynvdijke/traces/commit/4d6d041fc54d1f9718acffabf235eea621489acf))
* initialize location map on modal open and persist weather data ([0b6909c](https://github.com/martynvdijke/traces/commit/0b6909c675068327b77100a271198322e2faeeb0))

## [1.8.9](https://github.com/martynvdijke/traces/compare/v1.8.8...v1.8.9) (2026-05-06)


### Bug Fixes

* **deps:** update module golang.org/x/crypto to v0.50.0 ([df9eced](https://github.com/martynvdijke/traces/commit/df9ecedd72b51916ded8eac41dfb146a552ebf3b))

## [1.8.8](https://github.com/martynvdijke/traces/compare/v1.8.7...v1.8.8) (2026-05-06)


### Bug Fixes

* include CSRF token in image upload requests ([65ee3e2](https://github.com/martynvdijke/traces/commit/65ee3e2cbc9ef3e59e139c012bdc8d3fc4b18c8d))

## [1.8.7](https://github.com/martynvdijke/traces/compare/v1.8.6...v1.8.7) (2026-05-05)


### Bug Fixes

* event creation broken by missing Content-Type and null map refs ([9073025](https://github.com/martynvdijke/traces/commit/9073025dea1dcc09b650002ba3eb1ab6b6a70617))

## [1.8.6](https://github.com/martynvdijke/traces/compare/v1.8.5...v1.8.6) (2026-05-05)


### Bug Fixes

* compile TypeScript in Docker build ([2a0270d](https://github.com/martynvdijke/traces/commit/2a0270d52edf46d76d14aad0de31356672f56419))

## [1.8.5](https://github.com/martynvdijke/traces/compare/v1.8.4...v1.8.5) (2026-05-05)

## [1.8.4](https://github.com/martynvdijke/traces/compare/v1.8.3...v1.8.4) (2026-05-05)


### Bug Fixes

* add SHA256 password migration tests for login handler ([4566ac6](https://github.com/martynvdijke/traces/commit/4566ac6f1ef2cefa5965658a37f04a4e35f83442))

## [1.8.3](https://github.com/martynvdijke/traces/compare/v1.8.2...v1.8.3) (2026-05-05)


### Bug Fixes

* serve sw.js at root and add auth to admin page Playwright tests ([2e8731e](https://github.com/martynvdijke/traces/commit/2e8731e377f4d2d11f0e4faf6823ad02aa3da665))

## [1.8.2](https://github.com/martynvdijke/traces/compare/v1.8.1...v1.8.2) (2026-05-05)


### Bug Fixes

* **admin:** add db backup ([265c78d](https://github.com/martynvdijke/traces/commit/265c78d244eec2b751fd33773929f4405804514e))

## [1.8.1](https://github.com/martynvdijke/traces/compare/v1.8.0...v1.8.1) (2026-05-05)


### Bug Fixes

* migrate legacy SHA256 passwords to bcrypt on login ([a8d3965](https://github.com/martynvdijke/traces/commit/a8d39654bc0b081d6922292789f874f91e490eee))

# [1.8.0](https://github.com/martynvdijke/traces/compare/v1.7.1...v1.8.0) (2026-05-05)


### Bug Fixes

* Playwright E2E test failures and scanEventsWithPerson column mismatch ([47f49e5](https://github.com/martynvdijke/traces/commit/47f49e515c896a955689af90a3dd11f970c6dbc2)), closes [#timeline-tab](https://github.com/martynvdijke/traces/issues/timeline-tab)


### Features

* calendar view, recurring events, weather enrichment, AI auto-tagging, multi-user, markdown, PWA, and tabbed UI ([fe56391](https://github.com/martynvdijke/traces/commit/fe56391584d22c2f114c6f37f033f1d3929e0e91))

## [1.7.1](https://github.com/martynvdijke/traces/compare/v1.7.0...v1.7.1) (2026-05-04)


### Bug Fixes

* activity overview API, swagger docs, Umami analytics, and security audit fixes ([25d53ba](https://github.com/martynvdijke/traces/commit/25d53bae77a5a09d57fa3122d9a512cba979a568))

# [1.7.0](https://github.com/martynvdijke/traces/compare/v1.6.1...v1.7.0) (2026-05-04)


### Features

* person avatar upload, inline media picker, tag selector, and expanded tests ([463135a](https://github.com/martynvdijke/traces/commit/463135ac3f116a155247f20bff2589102deb8909))

## [1.6.1](https://github.com/martynvdijke/traces/compare/v1.6.0...v1.6.1) (2026-05-04)

# [1.6.0](https://github.com/martynvdijke/traces/compare/v1.5.0...v1.6.0) (2026-05-04)


### Features

* add memories feature with email support ([57e207e](https://github.com/martynvdijke/traces/commit/57e207e0650bae0840d7b60f560c7220024ec6db))

# [1.5.0](https://github.com/martynvdijke/traces/compare/v1.4.0...v1.5.0) (2026-05-04)


### Features

* photo uploader with mobile camera capture and title-only event creation ([e7ff821](https://github.com/martynvdijke/traces/commit/e7ff821763e5e52873f1058ba7475863431b8a62))

# [1.4.0](https://github.com/martynvdijke/traces/compare/v1.3.0...v1.4.0) (2026-05-03)


### Features

* enhanced search, autocomplete, person tracking, and modern UI redesign ([2e6efe5](https://github.com/martynvdijke/traces/commit/2e6efe55aaa0d13d259347e72ec6efd85180fc84))

# [1.3.0](https://github.com/martynvdijke/traces/compare/v1.2.0...v1.3.0) (2026-05-03)


### Features

* add geolocation, mobile UI improvements, and tests for hash-based upload ([2b7ff05](https://github.com/martynvdijke/traces/commit/2b7ff05d6c607b15570fb65e98083ae41d2d03e9))

# [1.2.0](https://github.com/martynvdijke/traces/compare/v1.1.1...v1.2.0) (2026-05-03)


### Features

* hash-based file storage with image resizing and thumbnail generation ([17c9538](https://github.com/martynvdijke/traces/commit/17c9538076f04fc062cffe4f01c528e724530c90))

## [1.1.1](https://github.com/martynvdijke/traces/compare/v1.1.0...v1.1.1) (2026-05-03)


### Bug Fixes

* restore activity feed, map, and add backend CRUD tests ([6fef529](https://github.com/martynvdijke/traces/commit/6fef52953ac966758578216c7353077c0fac8f9f))

# [1.1.0](https://github.com/martynvdijke/traces/compare/v1.0.0...v1.1.0) (2026-05-03)


### Bug Fixes

* align media route with Gin router and return empty arrays instead of null for JSON APIs ([cade074](https://github.com/martynvdijke/traces/commit/cade0740bb5b6618741e8ab2fe2f951c1249db51))
* align Playwright tests with updated UI and port 6270 ([580ea48](https://github.com/martynvdijke/traces/commit/580ea4836199465ceaa0e9a25c2b1f216313afe9))
* remove build files and fix CI webServer startup ([6cde9f9](https://github.com/martynvdijke/traces/commit/6cde9f90d8ad481d2aafefacddb80c81792e996d))
* update static media path from /static/media to /media ([3deacf0](https://github.com/martynvdijke/traces/commit/3deacf089bd250a0adf63184cd09634d3b184e24))


### Features

* migrate to Gin, add persons/map/Gotify/stats, responsive mobile, expanded media formats ([c5a5adb](https://github.com/martynvdijke/traces/commit/c5a5adbcb764207ff3c7aae075b26d3e8130dff7))

# 1.0.0 (2026-05-03)


### Bug Fixes

* **ci:** add in ci version stuff ([961e6d4](https://github.com/martynvdijke/traces/commit/961e6d453b67a9714cbe18dfeaa97729037fff36))
* **ci:** fix release process ([c7955ab](https://github.com/martynvdijke/traces/commit/c7955abe0a87f2be591f2689c0cdea5740bf4c99))


### Features

* add new features - search, tags, clone, import/export, stats, sharing ([e127a74](https://github.com/martynvdijke/traces/commit/e127a746061908b150b45d466a2df53d6f4abb90))
* add timeline events web application ([5a7416e](https://github.com/martynvdijke/traces/commit/5a7416e7d8c0b7f2e4a9710de283dedf677cad31))
* first working version ([5dcdd4f](https://github.com/martynvdijke/traces/commit/5dcdd4f68dba6c99fa5eadc1663ef5f0d54b0081))
