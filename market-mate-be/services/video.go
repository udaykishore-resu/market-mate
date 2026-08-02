package services

import (
	"context"
	"fmt"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// VideoServiceInterface defines the interface for video service operations
type VideoServiceInterface interface {
	GetVideoDetails(videoID string) (*youtube.Video, error)
}

type VideoService struct {
	youtubeService *youtube.Service
}

func NewVideoService(apiKey string) (*VideoService, error) {
	service, err := youtube.NewService(context.Background(), option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("error creating YouTube service: %v", err)
	}

	return &VideoService{
		youtubeService: service,
	}, nil
}

func (s *VideoService) GetVideoDetails(videoID string) (*youtube.Video, error) {
	call := s.youtubeService.Videos.List([]string{"snippet"}).Id(videoID)
	response, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("error fetching video details: %v", err)
	}

	if len(response.Items) == 0 {
		return nil, fmt.Errorf("video not found")
	}

	return response.Items[0], nil
}

func ExtractVideoID(url string) string {
	// Basic extraction for now - can be enhanced for different URL formats
	if len(url) == 11 {
		return url
	}
	return url[len(url)-11:]
}
