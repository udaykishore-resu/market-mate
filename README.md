# MarketMate

Paste a YouTube cooking video link and MarketMate pulls out the ingredient list, then shows nearby stores where you can buy them.

## How it works

The backend fetches the video's details from the YouTube Data API, sends the description to OpenAI (GPT-4) to extract a structured ingredient list with quantities, then looks up nearby grocery stores via the Google Maps API and returns both to the frontend.

## Project structure

market-mate-fe is the React frontend (Vite, TypeScript, shadcn-ui, Tailwind CSS). market-mate-be is the Go backend (Gin).

## Backend

### API

```
POST /api/process-video
Body: { "url": "<youtube-video-url>" }
Response: { "ingredients": [{ "name": "...", "quantity": "..." }], "stores": [{ "name": "...", "address": "...", "distance": "...", "mapUrl": "..." }] }
```

### Stack

Gin for HTTP routing and CORS, YouTube Data API for video details, OpenAI GPT-4 for ingredient extraction, Google Maps API for nearby stores, go-cache for response caching, IP-based location detection, a request logging middleware, and a per-IP rate limiter (100 requests/minute).

### Setup

Create a .env file in market-mate-be:

```
YOUTUBE_API_KEY=your_youtube_api_key
MAPS_API_KEY=your_google_maps_api_key
OPENAI_API_KEY=your_openai_api_key
PORT=8080
```

Then run it:

```bash
cd market-mate-be
go mod download
go run cmd/main.go
```

### Tests

```bash
cd market-mate-be
go test ./...
```

## Frontend

```bash
cd market-mate-fe
npm install
npm run dev
```

Runs on http://localhost:5173 by default, which is the origin the backend's CORS config allows.

## Status

The store lookup currently uses a fixed San Francisco coordinate as a placeholder rather than the detected user location end to end. That's the next real gap to close.
