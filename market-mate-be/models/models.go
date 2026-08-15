package models

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

// ErrorResponse is the single error shape the API returns.
type ErrorResponse struct {
	Error string `json:"error"`
	Stage string `json:"stage,omitempty"`
}
