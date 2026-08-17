# MarketMate API.
#
# At the repository root rather than inside market-mate-be/ so the build context
# is the whole repo, which is what the platform stack passes. The backend module
# lives one directory down and everything below reflects that.

FROM golang:1.24-alpine AS build
WORKDIR /src

# Dependency layer first: MarketMate has a large dependency graph (gin, the
# Google Maps client, the OpenAI client, pgx, go-redis), and re-downloading it
# on every source edit dominates the build.
COPY market-mate-be/go.mod market-mate-be/go.sum ./
RUN go mod download

COPY market-mate-be/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/marketmate ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/marketmate /app/marketmate

# Fixtures on by default so the image runs with no API keys at all. Every
# fixture response is labelled as simulated, so a demo can never be mistaken for
# live data — see the provenance handling in services/.
ENV PORT=8081 \
    USE_FIXTURES=true \
    GIN_MODE=release

EXPOSE 8081

HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/app/marketmate", "-health"]

USER nonroot:nonroot
ENTRYPOINT ["/app/marketmate"]
