package services

import (
	"errors"
	"strings"
	"testing"
)

const wantID = "dQw4w9WgXcQ"

func TestParseVideoID_Valid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"bare id", wantID},
		{"bare id with whitespace", "  " + wantID + "  "},

		{"watch url", "https://www.youtube.com/watch?v=" + wantID},
		{"watch url no www", "https://youtube.com/watch?v=" + wantID},
		{"watch url http", "http://www.youtube.com/watch?v=" + wantID},
		{"watch url no scheme", "youtube.com/watch?v=" + wantID},
		{"watch url mobile", "https://m.youtube.com/watch?v=" + wantID},
		{"watch url music", "https://music.youtube.com/watch?v=" + wantID},

		// The two forms YouTube's own share button produces. Both returned
		// garbage before this rewrite.
		{"share link with si param", "https://youtu.be/" + wantID + "?si=Ab1Cd2Ef3Gh4"},
		{"watch url with timestamp", "https://www.youtube.com/watch?v=" + wantID + "&t=30s"},

		{"short link", "https://youtu.be/" + wantID},
		{"short link no scheme", "youtu.be/" + wantID},
		{"shorts", "https://www.youtube.com/shorts/" + wantID},
		{"embed", "https://www.youtube.com/embed/" + wantID},
		{"embed nocookie", "https://www.youtube-nocookie.com/embed/" + wantID},
		{"live", "https://www.youtube.com/live/" + wantID},
		{"legacy /v/", "https://www.youtube.com/v/" + wantID},

		{"playlist context", "https://www.youtube.com/watch?v=" + wantID + "&list=PLabc123&index=4"},
		{"timestamp before v", "https://www.youtube.com/watch?t=90&v=" + wantID},
		{"trailing slash", "https://youtu.be/" + wantID + "/"},
		{"uppercase host", "https://WWW.YOUTUBE.COM/watch?v=" + wantID},
		{"embed with params", "https://www.youtube.com/embed/" + wantID + "?autoplay=1&mute=1"},
		{"shorts with params", "https://youtube.com/shorts/" + wantID + "?feature=share"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVideoID(tc.url)
			if err != nil {
				t.Fatalf("ParseVideoID(%q) returned error %v, want %q", tc.url, err, wantID)
			}
			if got != wantID {
				t.Errorf("ParseVideoID(%q) = %q, want %q", tc.url, got, wantID)
			}
		})
	}
}

func TestParseVideoID_Invalid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		// These two panicked before the rewrite.
		{"empty string", ""},
		{"short string", "short"},

		{"whitespace only", "   "},
		{"single char", "a"},
		{"ten chars", "dQw4w9WgXc"},
		{"twelve chars", "dQw4w9WgXcQQ"},
		{"invalid id characters", "dQw4w9WgXc!"},
		{"wrong host", "https://vimeo.com/123456789"},
		{"lookalike host", "https://notyoutube.com/watch?v=" + wantID},
		{"youtube host, no video", "https://www.youtube.com/"},
		{"youtube channel page", "https://www.youtube.com/@somechannel"},
		{"youtube results page", "https://www.youtube.com/results?search_query=pasta"},
		{"watch with empty v", "https://www.youtube.com/watch?v="},
		{"watch with bad v", "https://www.youtube.com/watch?v=tooshort"},
		{"youtu.be with no id", "https://youtu.be/"},
		{"shorts with bad id", "https://www.youtube.com/shorts/abc"},
		{"plain text", "how do I make carbonara"},
		{"control characters", "https://youtu.be/\x00\x01"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVideoID(tc.url)
			if err == nil {
				t.Errorf("ParseVideoID(%q) = %q with no error; want ErrInvalidVideoURL", tc.url, got)
			}
			if !errors.Is(err, ErrInvalidVideoURL) {
				t.Errorf("ParseVideoID(%q) error = %v, want ErrInvalidVideoURL", tc.url, err)
			}
			if got != "" {
				t.Errorf("ParseVideoID(%q) returned %q alongside an error; want empty", tc.url, got)
			}
		})
	}
}

// TestParseVideoID_NeverPanics is the regression guard for the defect class the
// old implementation had: it sliced url[len(url)-11:] with no bounds check, so
// any input shorter than 11 characters took the goroutine down. No input of any
// shape may panic.
func TestParseVideoID_NeverPanics(t *testing.T) {
	inputs := []string{
		"", " ", "a", "ab", "abc", "abcdefghij", "//", "://", "?", "&", "=",
		"http://", "https://", "https://youtu.be", "youtu.be", "youtube.com",
		"?v=", "&v=", "https://youtu.be/?si=", strings.Repeat("a", 10),
		strings.Repeat("x", 5000), "\x00", "\n\t", "%%%%", "://///",
		"https://youtu.be/%zz", "https://[::1]/watch?v=x",
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseVideoID(%q) panicked: %v", in, r)
				}
			}()
			_, _ = ParseVideoID(in)
		}()
	}
}

// TestExtractVideoID_LegacyWrapper confirms the deprecated signature still
// behaves for any caller not yet migrated.
func TestExtractVideoID_LegacyWrapper(t *testing.T) {
	if got := ExtractVideoID("https://youtu.be/" + wantID + "?si=xyz"); got != wantID {
		t.Errorf("ExtractVideoID() = %q, want %q", got, wantID)
	}
	if got := ExtractVideoID("nonsense"); got != "" {
		t.Errorf("ExtractVideoID(invalid) = %q, want empty string", got)
	}
}

func FuzzParseVideoID(f *testing.F) {
	f.Add("https://www.youtube.com/watch?v=" + wantID)
	f.Add("https://youtu.be/" + wantID + "?si=abc")
	f.Add("")
	f.Add("short")
	f.Fuzz(func(t *testing.T, in string) {
		id, err := ParseVideoID(in)
		// The contract: a nil error implies a well-formed ID, always.
		if err == nil && !videoIDPattern.MatchString(id) {
			t.Errorf("ParseVideoID(%q) returned %q with no error, which is not a valid ID", in, id)
		}
		if err != nil && id != "" {
			t.Errorf("ParseVideoID(%q) returned both %q and error %v", in, id, err)
		}
	})
}
