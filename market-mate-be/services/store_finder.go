// services/store_finder.go
package services

import (
	"context"
	"fmt"
	"market-mate/models"
	"market-mate/utils"

	"googlemaps.github.io/maps"
)

type StoreFinder struct {
	mapsClient *maps.Client
}

func NewStoreFinder(apiKey string) (*StoreFinder, error) {
	client, err := maps.NewClient(maps.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("error creating Maps client: %v", err)
	}
	return &StoreFinder{mapsClient: client}, nil
}

func (s *StoreFinder) FindNearbyStores(lat, lng float64) ([]models.Store, error) {
	ctx := context.Background()

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
		return nil, fmt.Errorf("error finding nearby stores: %v", err)
	}

	var stores []models.Store
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

	return stores, nil
}
