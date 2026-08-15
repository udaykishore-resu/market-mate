# MarketMate

Paste a YouTube cooking video link and MarketMate pulls out the ingredient list,
then shows nearby stores where you can buy them.

```bash
git clone https://github.com/udaykishore-resu/market-mate
cd market-mate
make demo
```

Open http://localhost:5173. **No API keys, no signup, no billing account.**

## How it works

The backend resolves the video ID from whatever link you paste, fetches the
video's details from the YouTube Data API, sends the description to OpenAI to
extract a structured ingredient list, geolocates you from your IP, and looks up
nearby grocery stores via the Google Places API. Results are cached for 15
minutes per video and location.

Each of those three external stages sits behind an interface with two
implementations: the live API client, and a fixture that returns realistic data.
Which one runs is decided per stage at startup by whether that stage's API key
is present. That is why the quick start above works with an empty environment —
and why the whole pipeline is testable without a network.

## Demo mode

| Keys present | Behaviour |
|---|---|
| none | Every stage uses fixtures. Fully working demo. |
| some | Those stages go live; the rest stay simulated. |
| all | Fully live. |

Simulated results are never disguised as real ones. The response carries a
`simulated` block naming which stages were fixtures, the UI shows a banner, and
`GET /api/health` reports the mode. `DEMO_MODE=true` forces fixtures even when
keys are present, which is useful for screenshots and deterministic tests.

## API

### `POST /api/process-video`

```json
{ "url": "https://youtu.be/dQw4w9WgXcQ?si=Ab1Cd2Ef3Gh4" }
```

Accepts every YouTube link form: `watch?v=`, `youtu.be/`, `/shorts/`, `/embed/`,
`/live/`, `/v/`, a bare 11-character ID, and any of those carrying `?si=`, `&t=`,
or `&list=` parameters.

```json
{
  "ingredients": [{ "name": "Spaghetti", "quantity": "400 g" }],
  "stores": [{
    "name": "Whole Foods Market",
    "address": "100 Market Street",
    "distance": "0.5 km",
    "mapUrl": "https://www.google.com/maps/search/?api=1&query=37.778,-122.416"
  }],
  "video": { "id": "...", "title": "...", "channelTitle": "...", "thumbnailUrl": "..." },
  "location": { "latitude": 37.77, "longitude": -122.42, "label": "San Francisco, CA", "estimated": false },
  "simulated": { "video": false, "ingredients": false, "stores": false, "any": false },
  "cached": false,
  "notice": ""
}
```

Errors return `{"error": "...", "stage": "url|video|ingredients|stores"}` with an
appropriate status: 400 for a bad URL, 429 (with `Retry-After`) when rate
limited, 502 when an upstream provider fails.

### `GET /api/health`

Reports status, whether each provider is `live` or `simulated`, and cache stats.

## Configuration

Everything is optional — see `market-mate-be/.env.example`.

| Variable | Default | Purpose |
|---|---|---|
| `YOUTUBE_API_KEY` | — | Live video metadata |
| `OPENAI_API_KEY` | — | Live ingredient extraction |
| `MAPS_API_KEY` | — | Live store search |
| `PORT` | `8080` | Backend listen port |
| `ALLOWED_ORIGINS` | `localhost:5173,localhost:4173` | CORS allow-list |
| `DEMO_MODE` | `false` | Force fixtures even with keys |

Frontend: `VITE_API_BASE_URL` (default empty = same origin; the dev and preview
servers proxy `/api` to the backend).

## Development

```bash
make demo      # both services, no keys needed
make dev-be    # backend only  → :8080
make dev-fe    # frontend only → :5173
make test      # backend tests with race detector and coverage
make lint      # go vet + tsc --noEmit
make build     # static binary + production bundle
```

Tests need no keys and make no network calls; they run the real handler against
the fixture providers.

## Deployment

```bash
docker compose up --build     # → http://localhost:8081
```

The backend builds to a static CGO-free binary on a distroless base and probes
its own health endpoint via `market-mate -health` (distroless has no shell). The
frontend builds to static files served by nginx, which proxies `/api` to the
backend so the browser stays same-origin and CORS never applies in production.

For a platform deploy, the backend is a single binary reading `PORT` and the
frontend is a static `dist/` directory — both fit Render, Fly.io, Railway, or
Cloud Run without modification.

## Project structure

```
market-mate-be/            Go 1.21 + Gin
  cmd/                     entrypoint, provider selection, graceful shutdown
  handlers/                HTTP handlers + tests
  services/                providers (live + fixture), URL parsing, cache, location
  middleware/              request logging, per-IP rate limiting
  models/                  API types
market-mate-fe/            React 18 + Vite + TypeScript + shadcn-ui + Tailwind
specs/                     spec-driven development docs
```

## Notes on this iteration

The store lookup no longer uses a fixed San Francisco coordinate. It geolocates
from the client IP with a bounded timeout, skips the lookup entirely for private
and loopback addresses, and marks the result `estimated` when it falls back. The
response cache and the location service were both already constructed and
injected into the handler but never called; both are now live.

The URL parser was rewritten. The previous implementation took the last 11
characters of the input string, which returned garbage for any link carrying
query parameters — including the `?si=` that YouTube's share button appends by
default — and panicked outright on input shorter than 11 characters. It is now a
real parser with table-driven tests and a fuzz target.
