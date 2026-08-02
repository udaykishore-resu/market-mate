package services

import (
	"testing"

	"google.golang.org/api/youtube/v3"
)

// MockVideoService implements VideoService interface for testing
type MockVideoService struct {
	GetVideoDetailsFn func(videoID string) (*youtube.Video, error)
}

func (m *MockVideoService) GetVideoDetails(videoID string) (*youtube.Video, error) {
	if m.GetVideoDetailsFn != nil {
		return m.GetVideoDetailsFn(videoID)
	}
	return nil, nil
}

func TestExtractVideoID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "direct video ID",
			url:      "dQw4w9WgXcQ",
			expected: "dQw4w9WgXcQ",
		},
		{
			name:     "full YouTube URL",
			url:      "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			expected: "dQw4w9WgXcQ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractVideoID(tt.url)
			if result != tt.expected {
				t.Errorf("ExtractVideoID() = %v, want %v", result, tt.expected)
			}
		})
	}
}
