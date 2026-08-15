# Tasks: A MarketMate That Actually Runs

**Input**: Design documents from `/specs/001-demoable-pipeline/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md)

**Tests**: Included. The spec's whole premise is that untested code shipped
defects, so test tasks are mandatory here rather than optional.

**Organization**: Grouped by user story so each is independently deliverable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US4 per spec.md, or FOUND for shared prerequisites

## Path Conventions

Web app: `market-mate-be/` (Go + Gin), `market-mate-fe/` (React + Vite).

---

## Phase 1: Setup

**Purpose**: Repo hygiene that everything else depends on.

- [x] T001 Add root `.gitignore` covering `node_modules/`, `dist/`, `.env`, build output
- [x] T002 Untrack the committed `market-mate-fe/node_modules` (20,809 files) via `git rm -r --cached`
- [x] T003 [P] Add `market-mate-be/.env.example` documenting every variable as optional
- [x] T004 [P] Add root `Makefile` with `demo`, `dev-be`, `dev-fe`, `test`, `lint`, `build`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The provider seam. Nothing in US1–US4 can be built or tested until
the handler depends on interfaces (Constitution VI).

**⚠️ BLOCKS ALL USER STORIES**

- [x] T005 [FOUND] Define `VideoProvider`, `IngredientProvider`, `StoreProvider`, `LocationProvider` and the local `VideoDetails` type in `market-mate-be/services/providers.go`
- [x] T006 [FOUND] Add compile-time interface assertions for every implementation in `providers.go`
- [x] T007 [FOUND] Adapt `VideoService` to `VideoProvider` — accept `ctx`, return `*VideoDetails` instead of the Google SDK type, in `services/video.go`
- [x] T008 [FOUND] Adapt `StoreFinder` to `StoreProvider` — accept caller `ctx` instead of `context.Background()`, in `services/store_finder.go`
- [x] T009 [FOUND] Adapt `IngredientExtractor` to `IngredientProvider` in `services/ingredient_extractor.go`
- [x] T010 [FOUND] Change `handlers.VideoHandlerConfig` to hold interfaces rather than concrete pointers, in `handlers/video.go`
- [x] T011 [FOUND] Add `models.Provenance`, `models.SearchLocation`, `models.Video`, `models.ErrorResponse` to `models/models.go`, keeping `ingredients` and `stores` shape unchanged for existing clients

**Checkpoint**: The handler is now constructible in a test without any API key.

---

## Phase 3: US1 — See it work without signing up for anything (P1)

**Goal**: Full pipeline runs with an empty environment (FR-001, FR-002, FR-003).

**Independent test**: Unset every variable, start the backend, submit a URL,
confirm ingredients and stores render and are labelled simulated.

- [x] T012 [US1] Implement `FixtureVideoProvider` with a 5-recipe catalogue keyed by a stable FNV hash of the video ID, in `services/fixtures.go`
- [x] T013 [US1] Implement `FixtureIngredientProvider`, falling back to `parseIngredientLines` for unknown descriptions
- [x] T014 [US1] Implement `FixtureStoreProvider` synthesising stores around the supplied coordinate with real distances and working map links
- [x] T015 [US1] Make every config field optional with working defaults; add per-provider `HasYouTube`/`HasOpenAI`/`HasMaps` and `DEMO_MODE`, in `config/config.go`
- [x] T016 [US1] Implement per-provider live/fixture selection in `cmd/main.go`, logging the mode at startup
- [x] T017 [US1] Populate the `simulated` block and `notice` on every response, in `handlers/video.go`
- [x] T018 [US1] Add `GET /api/health` reporting status, per-provider mode, and cache stats
- [x] T019 [P] [US1] Frontend `DemoBanner` rendering the provenance notice, in `src/pages/Index.tsx`
- [x] T020 [US1] Handler test: full pipeline returns usable data with fixture providers
- [x] T021 [US1] Handler test: fixture responses are flagged simulated; live responses are not
- [x] T022 [US1] Handler test: `/api/health` reports `demo` vs `live` correctly

**Checkpoint**: `make demo` reaches a working browser demo with no keys.

---

## Phase 4: US2 — Paste any YouTube link and have it work (P1)

**Goal**: Every URL form resolves; no input panics (FR-004, FR-005, FR-006).

**Independent test**: Feed the parser every documented form plus hostile input.

- [x] T023 [US2] Implement `ParseVideoID` in `services/videoid.go` — bare ID, `watch?v=`, `youtu.be/`, `/shorts/`, `/embed/`, `/live/`, `/v/`; host allow-list; 11-char alphabet validation; error return
- [x] T024 [US2] Remove the old `url[len(url)-11:]` implementation from `services/video.go`; keep a deprecated `ExtractVideoID` wrapper for callers not yet migrated
- [x] T025 [US2] Return HTTP 400 with a specific message on parse failure, in `handlers/video.go`
- [x] T026 [P] [US2] Mirror the parser in the frontend so validation is instant; remove the TikTok check that accepted an unsupported host and rejected `youtu.be`, in `src/components/VideoInput.tsx`
- [x] T027 [US2] Table test over every valid URL form, including `?si=` and `&t=` variants
- [x] T028 [US2] Table test over invalid input, asserting `ErrInvalidVideoURL` and an empty return
- [x] T029 [US2] Regression test: no input panics — pins the empty-string and short-string cases
- [x] T030 [US2] Fuzz target asserting the contract "nil error implies a well-formed ID"
- [x] T031 [US2] Handler test: every URL form resolves to the same video through the API

**Checkpoint**: The two forms YouTube's share button produces both work.

---

## Phase 5: US3 — Stores near the user, not near San Francisco (P2)

**Goal**: Search centres on the client (FR-007, FR-008).

**Independent test**: Vary the client IP; confirm the coordinates change.

- [x] T032 [US3] Rewrite `LocationService.Resolve` with a 2s context timeout, private/loopback/link-local short-circuit, and a documented fallback, in `services/location.go`
- [x] T033 [US3] Call the location provider from the handler and delete the hardcoded `37.7749, -122.4194`
- [x] T034 [US3] Return `location` with an `estimated` flag on every response
- [x] T035 [P] [US3] Display "near {label} (estimated)" above the store list, in `src/pages/Index.tsx`
- [x] T036 [US3] Sort stores nearest-first in both the fixture and live providers
- [x] T037 [US3] Handler test: a stubbed Berlin location produces Berlin coordinates and Berlin store links
- [x] T038 [US3] Handler test: an unresolved location is flagged `estimated`

---

## Phase 6: US4 — Repeat lookups are fast (P3)

**Goal**: Cache hits call no providers (FR-009).

- [x] T039 [US4] Add `ResultKey(videoID, lat, lng)` rounding coordinates to 2dp, and hit/miss counters, in `services/cache.go`
- [x] T040 [US4] Read and write the cache around the pipeline in `handlers/video.go`; set `cached: true` on hits
- [x] T041 [US4] Handler test: a second identical request is served from cache with zero provider calls (verified by counting wrappers)
- [x] T042 [US4] Handler test: a different location does not hit another location's entry

---

## Phase 7: Polish & Deployment

- [x] T043 [P] Return 429 with a `Retry-After` header from the rate limiter
- [x] T044 [P] Make CORS origins configurable via `ALLOWED_ORIGINS`
- [x] T045 [P] Make the frontend API base configurable via `VITE_API_BASE_URL`, defaulting to same-origin
- [x] T046 Move the Vite dev server off port 8080 (collided with the backend) and proxy `/api`
- [x] T047 [P] Graceful shutdown and server timeouts in `cmd/main.go`
- [x] T048 [P] `-health` flag so the distroless image can health-check itself without a shell
- [x] T049 [P] Backend `Dockerfile` (static CGO-free binary on distroless)
- [x] T050 [P] Frontend `Dockerfile` + `nginx.conf` proxying `/api` and forwarding `X-Forwarded-For`
- [x] T051 [P] `docker-compose.yml` running both with no keys required
- [x] T052 Rewrite `README.md` around the no-keys quick start
- [x] T053 Verify: `go test ./... -race -cover`, `go vet`, frontend build, end-to-end run with an empty environment

---

## Dependencies

```
Setup (T001-T004)
   ↓
Foundational (T005-T011)  ← blocks everything
   ↓
   ├── US1 (T012-T022)  ── independently deliverable
   ├── US2 (T023-T031)  ── independently deliverable
   ├── US3 (T032-T038)  ── depends on US1's provider selection
   └── US4 (T039-T042)  ── depends on US3 (cache key includes location)
   ↓
Polish (T043-T053)
```

US1 and US2 are genuinely parallel after the foundation. US3 needs US1 in place
because the fixture store provider is what makes location observable without a
Maps key. US4 keys on location, so it follows US3.

## Status

All 53 tasks complete. 83 tests, race-clean, 94% statement coverage on
`handlers`, no network or API keys required to run the suite.
