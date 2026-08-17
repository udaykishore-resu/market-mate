# MarketMate backend

Go 1.21 + Gin. Resolves a YouTube link to a recipe, extracts the ingredients,
and finds nearby shops that stock them.

```bash
go run ./cmd     # http://localhost:8080, no keys and no infrastructure needed
```

## Every dependency is optional

Two independent axes decide what the service does, and both are resolved per
capability at startup rather than all-or-nothing.

**Providers** — which API key is present decides whether a stage is live or a
fixture. A fixture result is always labelled as one, in the REST response's
`simulated` block, in the `provenance` field of every GraphQL type that can
carry it, and in the stored and indexed records.

**Infrastructure** — which `MM_*` variable is set decides where data is kept:

| Unset | Falls back to |
|---|---|
| `MM_POSTGRES_DSN` | no durable cache; every miss re-fetches and re-extracts |
| `MM_REDIS_ADDR` | the in-process `go-cache` — not shared, not durable |
| `MM_ELASTIC_URL` | substring search over Postgres; no search at all if Postgres is also unset |

One line at boot says which way each of them resolved:

```
providers — video: simulated, ingredients: simulated, stores: simulated
dependencies — store: postgres, cache: redis, search: elasticsearch
extraction model version: gpt-4o-mini@6f2a91c30bb4
```

## What is cached, and for how long

A video's transcript and the ingredient list extracted from it **never change**.
They used to be discarded after fifteen minutes, so every repeat lookup paid
YouTube for a transcript that could not have moved and paid the model to read it
again. They are now rows in Postgres with no expiry:

- `videos` — transcript, title, channel, duration, source
- `extractions` — ingredients as `JSONB`, keyed by `(video_id, model_version)`
- `recipes` — a view joining the two, newest extraction per video

`model_version` is `<model>@<prompt fingerprint>`, where the fingerprint is a
short sha256 of the extraction prompt computed at startup. Editing the prompt
therefore invalidates cleanly: the new fingerprint simply has no rows, so the
service re-extracts rather than serving output the current prompt would never
produce. Rolling the prompt back finds its own rows again.

Store lookups **are** time-sensitive — shops close, and their hours change — so
they keep a TTL (`MM_STORE_CACHE_TTL`, default 15m) in Redis. Their cache key
carries a precision-5 geohash (~5km cell) rather than a rounded coordinate:
rounding split neighbours who happened to fall on either side of a boundary, so
two clients 20 metres apart could each mint a private entry.

Migrations are embedded with `embed.FS` and applied on boot when `MM_MIGRATE` is
true. Several replicas can boot at once: they serialise on a Postgres advisory
lock and record what they applied in `schema_migrations`.

## API

### `POST /api/process-video`

```json
{ "url": "https://youtu.be/dQw4w9WgXcQ?si=Ab1Cd2Ef3Gh4" }
```

Accepts every YouTube link form. Returns the ingredient list, nearby shops, the
search location, and a `simulated` provenance block. Errors are
`{"error": "...", "stage": "url|video|ingredients|stores"}` with 400 for a bad
URL, 429 when rate limited, and 502 when an upstream provider fails.

### `GET /api/recipes/search?q=&ingredient=&limit=`

Free-text search over titles, channels and ingredient names. `ingredient` may be
repeated to filter to recipes using any of the named ingredients. The response
names the backend that answered (`elasticsearch`, `postgres` or `disabled`), so
an empty result is never ambiguous.

### `POST /api/admin/reindex`

Replays every recipe from Postgres into Elasticsearch. Postgres is the system of
record and the index is derived, so this is the repair tool for the one thing
that can go wrong with derived state: having missed a write. 503 without
Postgres, since there would be nothing to replay from.

### `GET /api/health`

```json
{
  "status": "ok",
  "mode": "demo",
  "checks": {
    "postgres": {"ok": true, "state": "up", "impl": "postgres", "latency_ms": 2},
    "redis": {"ok": true, "state": "disabled", "impl": "memory", "latency_ms": 0},
    "elasticsearch": {"ok": true, "state": "disabled", "impl": "postgres", "latency_ms": 0},
    "providers": {"transcript": "fixture", "ingredients": "fixture", "stores": "fixture"}
  },
  "model_version": "fixture@dc25fcb65b35"
}
```

`impl` is the field to read: the useful question is not "is Redis up" but "is
this deployment using the Redis I configured". An unconfigured dependency is
`disabled` and `ok` — opting out is a choice, not a fault. A configured
dependency that fails its probe makes the status `degraded`, still with a 200:
the service answers from its providers regardless, and failing the pod over a
cache outage would turn a slow demo into an outage. The only 503 is during
shutdown, while the server drains.

### `POST /graphql`, `GET /graphiql`

```graphql
{
  recipe(videoId: "dQw4w9WgXcQ") {
    title
    provenance { source simulated notice }
    ingredients {
      name
      stores(lat: 37.7749, lng: -122.4194, radiusMeters: 2000) { name distance }
    }
  }
}
```

Resolving `stores` on every ingredient of a recipe costs one provider call, not
one per ingredient: all of them route through a shared lookup that collapses
concurrent calls and caches by geohash cell. Also `searchRecipes`, a top-level
`stores`, and `health`, which resolves through the same checker as the REST
endpoint so the two cannot disagree. GraphiQL is off unless `MM_GRAPHIQL=true` —
it is an unauthenticated schema browser.

## Configuration

See `.env.example`. Everything is optional; the platform-wide names and values
live in the dev-stack contract.

## Testing

```bash
go test ./... -race
go vet ./...
```

No keys, no network, no containers: the handler tests run the real handler
against the fixture providers, and the Elasticsearch client is tested against an
`httptest` server.

The storage package is the exception. Its tests exercise the advisory lock, the
`ON CONFLICT` clauses, the JSONB round trip and the view's `DISTINCT ON` — none
of which exist outside Postgres, so a fake would only test the fake. They skip
unless `MM_TEST_POSTGRES_DSN` is set, and the skip message says how to start
one:

```bash
docker run --rm -e POSTGRES_PASSWORD=marketmate -e POSTGRES_USER=marketmate \
  -e POSTGRES_DB=marketmate -p 5432:5432 postgres:17-alpine

MM_TEST_POSTGRES_DSN='postgres://marketmate:marketmate@localhost:5432/marketmate?sslmode=disable' \
  go test ./storage/ -v
```

## Layout

```
cmd/          entrypoint: provider selection, dependency resolution, graceful shutdown
config/       environment parsing; every value has a working default
handlers/     HTTP handlers for the pipeline, search, reindex and health
services/     providers (live + fixture), URL parsing, cache, geohash, store lookup
storage/      Postgres store, embedded migrations
search/       Elasticsearch client and the Postgres fallback
gql/          GraphQL schema and resolvers
health/       dependency probes, shared by REST and GraphQL
models/       API and record types
middleware/   request logging, per-IP rate limiting
```
