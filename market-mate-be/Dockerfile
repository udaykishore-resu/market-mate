# Build a static, CGO-free binary and ship it on a distroless base.
FROM golang:1.23-alpine AS build

WORKDIR /src

# Copy manifests first so the dependency layer caches independently of source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/market-mate ./cmd

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/market-mate /app/market-mate

# No keys are baked in. With none supplied at runtime the service starts in
# demo mode rather than failing, so this image is directly demoable.
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/app/market-mate"]
