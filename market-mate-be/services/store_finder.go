// Package services holds the pipeline stages: video resolution, transcript
// retrieval, ingredient extraction and store lookup. Each external stage is an
// interface with a live and a fixture implementation, chosen at startup.
package services

import (
	"context"
	"fmt"
	"market-mate/models"
	"market-mate/utils"
	"math"
	"sort"
	"strconv"
	"strings"

	"googlemaps.github.io/maps"
)

type StoreFinder struct {
	mapsClient *maps.Client
}

func NewStoreFinder(apiKey string) (*StoreFinder, error) {
	client, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("creating the Maps client: %w", err)
	}
	return &StoreFinder{mapsClient: client}, nil
}

// FindNearbyStores implements StoreProvider. The context is now supplied by the
// caller rather than created here, so a cancelled request stops the upstream
// Maps call instead of leaving it running.
func (s *StoreFinder) FindNearbyStores(ctx context.Context, lat, lng float64) ([]models.Store, error) {
	r := &maps.NearbySearchRequest{
		Location: &maps.LatLng{
			Lat: lat,
			Lng: lng,
		},
		Radius:  5000, // 5km radius
		Keyword: "grocery store",
	}

	resp, err := s.mapsClient.NearbySearch(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("finding nearby stores: %w", err)
	}

	// Non-nil empty slice rather than nil: encoding/json renders a nil slice as
	// `null`, and the frontend maps over `stores` directly.
	stores := []models.Store{}
	for _, place := range resp.Results {
		distance := utils.CalculateDistance(lat, lng, place.Geometry.Location.Lat, place.Geometry.Location.Lng)

		store := models.Store{
			Name:     place.Name,
			Address:  place.Vicinity,
			Distance: fmt.Sprintf("%.1f km", distance),
			MapURL:   fmt.Sprintf("https://www.google.com/maps/place/?q=place_id:%s", place.PlaceID),
		}
		stores = append(stores, store)
	}

	// Nearest first, matching the fixture provider and what "nearby stores"
	// implies. The Places API orders by its own relevance ranking, not distance.
	sort.Slice(stores, func(i, j int) bool {
		return parseKm(stores[i].Distance) < parseKm(stores[j].Distance)
	})

	return stores, nil
}

// parseKm reads the "%.1f km" string back into a number for sorting. The
// formatted string is what the API returns, so this keeps one source of truth
// rather than carrying a parallel float field.
func parseKm(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, " km")), 64)
	if err != nil {
		return math.MaxFloat64
	}
	return v
}
