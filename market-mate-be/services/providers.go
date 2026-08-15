package services

import (
	"context"

	"market-mate/models"
)

// The provider interfaces are the seam that makes MarketMate testable and
// demoable.
//
// Before this, handlers.VideoHandlerConfig held three concrete struct pointers
// whose constructors each built a live API client on the spot. There was no way
// to instantiate the handler in a test, which is why the URL-parser panic and
// the hardcoded San Francisco coordinate both survived in main. One interface
// per capability fixes that: the handler depends on behaviour, and main decides
// at startup whether that behaviour comes from a live client or a fixture.

// VideoDetails is the subset of video metadata the pipeline needs.
//
// Deliberately a local struct rather than *youtube.Video: returning the Google
// SDK type through the interface would force every implementation, including
// the fixtures and the tests, to depend on the YouTube client library.
type VideoDetails struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ChannelTitle string `json:"channelTitle"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

// VideoProvider fetches metadata for a video.
type VideoProvider interface {
	GetVideoDetails(ctx context.Context, videoID string) (*VideoDetails, error)
}

// IngredientProvider turns a free-text description into a structured list.
type IngredientProvider interface {
	ExtractIngredients(ctx context.Context, description string) ([]models.Ingredient, error)
}

// StoreProvider finds retailers near a coordinate.
type StoreProvider interface {
	FindNearbyStores(ctx context.Context, lat, lng float64) ([]models.Store, error)
}

// LocationProvider resolves a client IP to a coordinate. The bool reports
// whether the result is a real resolution (true) or the documented fallback
// (false), so the response can tell the user which they got.
type LocationProvider interface {
	Resolve(ctx context.Context, ip string) (Location, bool)
}

// Compile-time assertions that every implementation satisfies its interface.
// Without these, a signature drift would only surface in main.
var (
	_ VideoProvider      = (*VideoService)(nil)
	_ VideoProvider      = (*FixtureVideoProvider)(nil)
	_ IngredientProvider = (*IngredientExtractor)(nil)
	_ IngredientProvider = (*FixtureIngredientProvider)(nil)
	_ StoreProvider      = (*StoreFinder)(nil)
	_ StoreProvider      = (*FixtureStoreProvider)(nil)
	_ LocationProvider   = (*LocationService)(nil)
)
