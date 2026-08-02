package services

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type LocationService struct{}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func NewLocationService() *LocationService {
	return &LocationService{}
}

func (ls *LocationService) GetLocationFromIP(ip string) (*Location, error) {
	resp, err := http.Get(fmt.Sprintf("https://ipapi.co/%s/json/", ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &Location{
		Latitude:  result.Latitude,
		Longitude: result.Longitude,
	}, nil
}
