# Feature Specification: A MarketMate That Actually Runs

**Feature Branch**: `001-demoable-pipeline`

**Created**: 2026-08-14

**Status**: Draft

**Input**: MarketMate exists but cannot be demonstrated. It requires three paid
API keys before it will start, its URL parser crashes on ordinary input, and the
store lookup it advertises is hardcoded to San Francisco.

---

## Problem

MarketMate's idea is good and its pipeline is real: paste a cooking video, get
the ingredient list, get somewhere nearby to buy them. But nobody can see it
work. Cloning the repo and following the README gets you a process that exits at
startup unless you have already provisioned a YouTube Data API key, an OpenAI
key with GPT-4 access, and a billed Google Maps key. That is a hard stop between
a curious person and a working product, and it is also a hard stop for the
project's own tests — which is why the defects below survived.

This iteration makes MarketMate runnable, correct on real input, and honest
about location.

### Defects confirmed by direct observation

Each was reproduced against the current `main` before this spec was written:

| Input | Current result | Should be |
|---|---|---|
| `https://youtu.be/dQw4w9WgXcQ?si=abc123` | `"Q?si=abc123"` | `dQw4w9WgXcQ` |
| `https://youtube.com/watch?v=dQw4w9WgXcQ&t=30s` | `"WgXcQ&t=30s"` | `dQw4w9WgXcQ` |
| `"short"` | **panic**: slice bounds out of range [-6:] | a clean validation error |
| `""` | **panic**: slice bounds out of range [-11:] | a clean validation error |

The `?si=` form is what YouTube's own share button produces by default, and the
`&t=` form is what it produces when sharing at a timestamp. So the two most
common ways a real person obtains a link are both broken, and a short string in
the input box takes a goroutine down.

Three further gaps, visible by reading rather than running:

- `LocationService` is constructed and injected, then never called. The handler
  passes a literal `37.7749, -122.4194` with a comment acknowledging it. Every
  user in the world is told about grocery stores in San Francisco.
- `CacheService` is constructed and injected, then never called. The README
  advertises response caching that does not happen.
- There is no health endpoint, so no deploy target can probe the service.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 — See it work without signing up for anything (Priority: P1)

Someone clones the repository, runs two commands, opens a browser, pastes a
cooking video link, and watches the full pipeline produce ingredients and
stores — with no API keys, no billing account, and no signup.

**Why this priority**: This is the difference between a repository and a
product. It is also the precondition for every other story: without a runnable
service there is no way to test the pipeline end to end.

**Independent Test**: With no environment variables set at all, start the
backend and frontend, submit a video URL, and confirm ingredients and stores
render.

**Acceptance Scenarios**:

1. **Given** no API keys in the environment, **When** the server starts,
   **Then** it starts successfully in demonstration mode and logs plainly that
   it is serving fixture data rather than live results.
2. **Given** the server is in demonstration mode, **When** a valid video URL is
   submitted, **Then** a realistic ingredient list and store list are returned,
   and the response states that the data is simulated.
3. **Given** all three API keys are present, **When** the server starts,
   **Then** it uses the live providers and does not mention demonstration mode.
4. **Given** only some keys are present, **When** the server starts, **Then** it
   reports exactly which providers are live and which are simulated, and runs
   with a mix rather than refusing to start.

---

### User Story 2 — Paste any YouTube link and have it work (Priority: P1)

Whatever form the link takes — copied from the address bar, produced by the
share button, a Shorts link, an embed, with or without a timestamp — the
ingredient extraction runs against the right video. Input that is not a YouTube
link is refused politely instead of crashing anything.

**Why this priority**: The entire product is entered through this one text
field. A parser that mishandles the output of YouTube's own share button fails
most first-time users on their first attempt.

**Independent Test**: Feed the parser the full catalogue of URL shapes plus
hostile input, and assert on the extracted ID; assert no input can panic.

**Acceptance Scenarios**:

1. **Given** any of `watch?v=`, `youtu.be/`, `/shorts/`, `/embed/`, `/live/`, or
   a bare 11-character ID, **When** parsed, **Then** the correct video ID is
   returned.
2. **Given** extra query parameters such as `?si=`, `&t=`, `&list=`, **When**
   parsed, **Then** they are ignored and the ID is still correct.
3. **Given** an empty string, a short string, or a non-YouTube URL, **When**
   parsed, **Then** an error is returned and no panic occurs.
4. **Given** an invalid URL is posted to the API, **When** the request is
   handled, **Then** the response is 400 with a message naming the problem.

---

### User Story 3 — Stores near the user, not near San Francisco (Priority: P2)

The store list reflects roughly where the requester actually is, determined from
their IP, with a stated fallback when that cannot be determined.

**Why this priority**: "Nearby stores" that are two thousand miles away is worse
than no feature. It is P2 only because the pipeline must run at all before its
location can be correct.

**Independent Test**: Issue requests with different forwarded client IPs and
confirm the coordinates used for the store search change accordingly.

**Acceptance Scenarios**:

1. **Given** a request from a routable IP, **When** stores are looked up,
   **Then** the search is centred on the coordinates resolved for that IP.
2. **Given** IP resolution fails or times out, **When** stores are looked up,
   **Then** a documented default location is used and the response states that
   the location was estimated.
3. **Given** a request from a private or loopback address, **When** stores are
   looked up, **Then** resolution is skipped without a wasted network call.

---

### User Story 4 — Repeat lookups are fast (Priority: P3)

Submitting the same video twice returns the second result immediately, without
re-billing the OpenAI and YouTube calls.

**Why this priority**: A correctness and cost win, not a visible feature. The
cache is already a dependency; it is simply never called.

**Acceptance Scenarios**:

1. **Given** a video processed once, **When** the same video is submitted again
   from the same location, **Then** the cached result is returned and no
   upstream provider is called.
2. **Given** a cached entry older than its TTL, **When** the video is submitted,
   **Then** the providers are called again.

---

### Edge Cases

- Every provider is unavailable → the request fails with a clear message naming
  which stage failed, never a bare 500.
- A video with no description, or a description with no ingredients → an empty
  ingredient list with an explanatory message, not an error.
- The AI returns prose instead of a list → parsed leniently; unparseable lines
  are skipped rather than corrupting the list.
- Location lookup hangs → bounded by a timeout, falls back, request still
  completes.
- Rate limit exceeded → 429 with a `Retry-After` header, not a silent drop.

---

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST start and serve requests with no environment variables
  configured.
- **FR-002**: System MUST select, per provider, between a live implementation
  and a fixture implementation based on whether that provider's key is present,
  and MUST report the selection at startup and over the API.
- **FR-003**: System MUST label any response containing simulated data as
  simulated.
- **FR-004**: System MUST correctly extract a video ID from every documented
  YouTube URL form, ignoring extraneous query parameters.
- **FR-005**: System MUST NOT panic on any input to the URL parser.
- **FR-006**: System MUST reject invalid URLs with HTTP 400 and a descriptive
  message.
- **FR-007**: System MUST determine the store-search location from the client's
  IP, with a bounded timeout and a documented fallback.
- **FR-008**: System MUST skip IP resolution for private and loopback addresses.
- **FR-009**: System MUST cache complete pipeline results keyed by video and
  location, and serve cache hits without calling providers.
- **FR-010**: System MUST expose a health endpoint reporting service status and
  per-provider live/simulated mode.
- **FR-011**: System MUST accept its allowed CORS origins from configuration.
- **FR-012**: System MUST return 429 with `Retry-After` when rate limited.
- **FR-013**: The frontend MUST take its API base URL from configuration rather
  than a hardcoded localhost literal.

### Key Entities

- **Ingredient** — a name and a quantity extracted from a video description.
- **Store** — a nearby retailer: name, address, distance, map link.
- **Provider** — an external capability (video metadata, ingredient extraction,
  store search) behind an interface, satisfied by either a live client or a
  fixture.
- **ProcessResult** — ingredients, stores, the resolved location, and the
  provenance flags saying which parts were simulated.

---

## Success Criteria *(mandatory)*

- **SC-001**: A fresh clone reaches a working browser demo in under two minutes
  with no credentials.
- **SC-002**: Every YouTube URL form in the table above resolves correctly, and
  no input panics.
- **SC-003**: Store coordinates vary with client IP.
- **SC-004**: A repeated request for the same video returns from cache with zero
  provider calls.
- **SC-005**: The backend has meaningful automated test coverage of the URL
  parser, the handler, and the fixture pipeline — none of which requires a
  network call or an API key to run.

## Out of Scope

Transcript-based extraction for videos whose descriptions omit ingredients;
user accounts and saved lists; real-time price or stock data; delivery
integration; a mobile app.
