package services

import (
	"context"
	"fmt"
	"strings"

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
	// contentDetails costs no extra quota unit on top of the snippet part and
	// carries the duration, which is stored with the transcript.
	call := s.youtubeService.Videos.List([]string{"snippet", "contentDetails"}).Id(videoID).Context(ctx)
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
	if cd := item.ContentDetails; cd != nil {
		details.DurationSeconds = ParseISO8601Duration(cd.Duration)
	}
	if details.ThumbnailURL == "" {
		details.ThumbnailURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
	}
	return details, nil
}

// ParseISO8601Duration reads YouTube's "PT14M12S" duration format into seconds.
//
// Only the time components are handled: the API returns days only for live
// streams, which this pipeline has nothing to say about anyway. An unparseable
// value yields 0, which the schema treats as "unknown" — refusing the whole
// video over a duration string would be a poor trade.
func ParseISO8601Duration(s string) int {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "P") {
		return 0
	}
	timePart := s
	if i := strings.Index(s, "T"); i >= 0 {
		timePart = s[i+1:]
	} else {
		return 0
	}

	total, num := 0, 0
	seen := false
	for _, r := range timePart {
		switch {
		case r >= '0' && r <= '9':
			num = num*10 + int(r-'0')
			seen = true
		case r == 'H':
			total += num * 3600
			num, seen = 0, false
		case r == 'M':
			total += num * 60
			num, seen = 0, false
		case r == 'S':
			total += num
			num, seen = 0, false
		default:
			return 0
		}
	}
	if seen {
		// Trailing digits with no unit: the string is not a duration.
		return 0
	}
	return total
}
