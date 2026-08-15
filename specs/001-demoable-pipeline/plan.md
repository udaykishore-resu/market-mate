# Implementation Plan: A MarketMate That Actually Runs

**Branch**: `001-demoable-pipeline` | **Spec**: [spec.md](./spec.md)

## Summary

Introduce a provider-interface layer between the handler and the three external
services, supply a fixture implementation of each, and select per provider at
startup based on key presence. Fix the URL parser. Wire the location and cache
services that are already constructed but never called. Keep the existing Gin
stack and the existing response shape.

## Technical Context

| | |
|---|---|
| **Backend** | Go 1.21, Gin (unchanged) |
| **New deps** | none — fixtures and parsing use the standard library |
| **Frontend** | React 18 + Vite + TypeScript + shadcn-ui + Tailwind (unchanged) |
| **Testing** | `testing` + `net/http/httptest`, no network, no keys |
| **Config** | env vars, all optional |

## The central decision: interfaces, then fixtures

Today `handlers.VideoHandlerConfig` holds three concrete struct pointers
(`*services.VideoService`, `*services.StoreFinder`,
`*services.IngredientExtractor`). Because they are concrete, and because each
constructor immediately builds a live API client, the handler cannot be
instantiated in a test at all. That is the root cause of the untested defects:
there was no seam to test through.

The fix is one small interface per capability, defined in the `services`
package and consumed by the handler:

```go
type VideoProvider      interface { GetVideoDetails(ctx, videoID) (*VideoDetails, error) }
type IngredientProvider interface { ExtractIngredients(ctx, description) ([]models.Ingredient, error) }
type StoreProvider      interface { FindNearbyStores(ctx, lat, lng float64) ([]models.Store, error) }
type LocationProvider   interface { Resolve(ctx, ip string) (Location, bool, error) }
```

Two implementations of each: the existing live client, and a fixture. Selection
happens once, in `main`, from config. Nothing else in the codebase learns about
demonstration mode — the handler cannot tell the difference, which is what makes
the fixture path a real test of the real pipeline rather than a parallel
codepath that can rot.

`VideoDetails` is a small local struct rather than `*youtube.Video`. Returning
the Google API type from the interface would force the fixture to import the
YouTube SDK and would leak a vendor type through the whole call graph.

### Provenance, not pretence

A fixture response must never be mistaken for a live one. `ProcessResult` carries
a `Simulated` block naming which providers were fixtures, the handler returns it,
and the UI shows a banner. The demo is honest about being a demo.

## Fixing the parser

`ExtractVideoID` is replaced by `ParseVideoID(string) (string, error)`:

1. Trim; if the input is exactly 11 valid ID characters, accept it.
2. Otherwise parse with `net/url`. Reject a non-YouTube host.
3. Dispatch on host and path: `watch?v=`, `youtu.be/{id}`, `/shorts/{id}`,
   `/embed/{id}`, `/live/{id}`, `/v/{id}`.
4. Validate the candidate against `^[A-Za-z0-9_-]{11}$`.
5. Return an error rather than a garbage string. No slicing without a bounds
   check, which is what removes the panic class entirely.

Returning an error instead of `""` is what lets the handler answer 400 with a
real message, satisfying FR-006.

## Location

`ProcessVideo` resolves the client IP through `LocationProvider` with a 2-second
timeout, skipping private and loopback ranges via `net.IP.IsPrivate()` and
friends. On failure it falls back to a documented default and marks the location
`Estimated: true`. The San Francisco literal disappears from the handler.

## Cache

Key: `videoID + rounded location`. Location is rounded to two decimal places
(~1km) so that trivially different IPs still share a cache entry instead of each
minting their own. The cached value is the whole `ProcessResult`, so a hit costs
zero provider calls.

## Files

```
market-mate-be/
  services/providers.go      NEW — the four interfaces + VideoDetails
  services/fixtures.go       NEW — fixture implementations + demo dataset
  services/videoid.go        NEW — ParseVideoID
  services/videoid_test.go   NEW — table tests incl. the panic cases
  services/video.go          live client adapted to VideoProvider
  services/store_finder.go   live client adapted to StoreProvider
  services/location.go       timeout, private-IP skip, LocationProvider
  handlers/video.go          depends on interfaces; location + cache wired
  handlers/video_test.go     NEW — httptest against fixtures
  config/config.go           per-provider mode, CORS origins
  cmd/main.go                provider selection, /api/health
market-mate-fe/
  src/services/api.ts        configurable base URL, typed errors
  src/pages/Index.tsx        provenance banner, real error states
```

## Phasing

1. `ParseVideoID` + tests. **Gate: the four reproduced defects turn green.**
2. Interfaces + fixtures; `main` selection; `/api/health`.
3. Handler rewrite: interfaces, location, cache. Handler tests.
4. Config: per-provider mode, CORS origins.
5. Frontend: configurable base URL, provenance banner, error states.
6. Build both, run end to end with no keys, screenshot.

## Risks

| Risk | Mitigation |
|---|---|
| Fixture path diverges from live path | One handler, one pipeline; only the leaf providers differ |
| A demo build reaching production unnoticed | Startup log, `/api/health` mode field, and a UI banner |
| Existing `ExtractVideoID` callers break | Thin wrapper retained during migration, removed once callers move |
