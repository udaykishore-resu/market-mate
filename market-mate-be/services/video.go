package services

import (
	"context"
	"fmt"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// VideoService is the live YouTube Data API implementation of VideoProvider.
type VideoService struct {
	youtubeService *youtube.Service
}

func NewVideoService(apiKey string) (*VideoService, error) {
	service, err := youtube.NewService(context.Background(), option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("creating YouTube service: %w", err)
	}
	return &VideoService{youtubeService: service}, nil
}

// GetVideoDetails fetches the snippet for a video and maps it onto the local
// VideoDetails type, so the Google SDK type does not leak past this file.
func (s *VideoService) GetVideoDetails(ctx context.Context, videoID string) (*VideoDetails, error) {
	call := s.youtubeService.Videos.List([]string{"snippet"}).Id(videoID).Context(ctx)
	response, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("fetching video details: %w", err)
	}
	if len(response.Items) == 0 {
		return nil, fmt.Errorf("video %s not found or is not public", videoID)
	}

	item := response.Items[0]
	details := &VideoDetails{ID: videoID}
	if sn := item.Snippet; sn != nil {
		details.Title = sn.Title
		details.Description = sn.Description
		details.ChannelTitle = sn.ChannelTitle
		if sn.Thumbnails != nil && sn.Thumbnails.High != nil {
			details.ThumbnailURL = sn.Thumbnails.High.Url
		}
	}
	if details.ThumbnailURL == "" {
		details.ThumbnailURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
	}
	return details, nil
}
