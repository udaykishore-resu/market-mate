package models

import "time"

type Store struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Distance string `json:"distance"`
	MapURL   string `json:"mapUrl"`
}

type Ingredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
}

// Video is the metadata shown alongside the results.
type Video struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ChannelTitle string `json:"channelTitle"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

// SearchLocation is the point the store search was centred on.
//
// Estimated is true when the client IP could not be resolved and the documented
// default was used, so the UI can say "near San Francisco (estimated)" rather
// than implying it knows where the user is.
type SearchLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Label     string  `json:"label,omitempty"`
	Estimated bool    `json:"estimated"`
}

// Provenance records which parts of a response came from fixtures rather than
// live providers. A demo response must never be mistakable for a real one.
type Provenance struct {
	Video       bool `json:"video"`
	Ingredients bool `json:"ingredients"`
	Stores      bool `json:"stores"`
	Any         bool `json:"any"`
}

// RecipeResponse is the API response.
//
// The original two fields keep their names and shape so existing clients are
// unaffected; everything below them is additive.
type RecipeResponse struct {
	Ingredients []Ingredient   `json:"ingredients"`
	Stores      []Store        `json:"stores"`
	Video       *Video         `json:"video,omitempty"`
	Location    SearchLocation `json:"location"`
	Simulated   Provenance     `json:"simulated"`
	Cached      bool           `json:"cached"`
	Notice      string         `json:"notice,omitempty"`
}

// Recipe is a persisted, searchable extraction: one video's ingredient list as
// it was stored, plus enough context to render a search hit without a second
// lookup.
//
// Source and Simulated travel with the record all the way into the search index
// and out through GraphQL. A fixture extraction that has been round-tripped
// through Postgres and Elasticsearch is still fixture data, and nothing
// downstream may present it otherwise.
type Recipe struct {
	VideoID      string       `json:"videoId"`
	Title        string       `json:"title"`
	Channel      string       `json:"channel"`
	Ingredients  []Ingredient `json:"ingredients"`
	Source       string       `json:"source"`
	Simulated    bool         `json:"simulated"`
	ModelVersion string       `json:"modelVersion,omitempty"`
	IndexedAt    time.Time    `json:"indexedAt"`
}

// SourceFixture and SourceYouTube label where a stored video came from. The
// label is part of the cache identity: a row written by the fixture provider is
// never served to a request being answered live, and vice versa.
const (
	SourceFixture = "fixture"
	SourceYouTube = "youtube"
)

// SearchResponse is the payload of GET /api/recipes/search.
type SearchResponse struct {
	Query   string   `json:"query"`
	Results []Recipe `json:"results"`
	// Total is how many results this response carries, not how many exist: the
	// endpoint returns one page and the backends do not agree on a cheap count.
	Total int `json:"total"`
	// Backend names the search implementation that answered — "elasticsearch",
	// "postgres" (the ILIKE fallback) or "disabled". Without it a caller cannot
	// tell an empty index from a search layer that is not running.
	Backend string `json:"backend"`
	Notice  string `json:"notice,omitempty"`
}

// ErrorResponse is the single error shape the API returns.
type ErrorResponse struct {
	Error string `json:"error"`
	Stage string `json:"stage,omitempty"`
}
