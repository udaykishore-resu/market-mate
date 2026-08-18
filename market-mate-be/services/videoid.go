package services

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrInvalidVideoURL is returned when the input cannot be resolved to a YouTube
// video ID. Callers map it to HTTP 400 — the previous implementation returned
// an empty string or a garbage substring, which the handler could not tell apart
// from a real ID.
var ErrInvalidVideoURL = errors.New("not a recognisable YouTube video URL")

// videoIDPattern is YouTube's ID alphabet: 11 chars of base64url.
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// youTubeHosts are the hosts we accept, after stripping any "www." prefix.
var youTubeHosts = map[string]bool{
	"youtube.com":          true,
	"m.youtube.com":        true,
	"music.youtube.com":    true,
	"youtube-nocookie.com": true,
	"youtu.be":             true,
}

// pathPrefixes are the path forms that carry the ID as the segment after the
// prefix: /shorts/ID, /embed/ID, /live/ID, /v/ID.
var pathPrefixes = []string{"shorts", "embed", "live", "v"}

// ParseVideoID extracts the 11-character video ID from any YouTube URL form.
//
// It replaces the previous ExtractVideoID, which did `url[len(url)-11:]`. That
// implementation had two failure modes, both reproduced before this rewrite:
// it panicked on any input shorter than 11 characters, and it returned trailing
// junk whenever the URL carried query parameters — including the "?si=" that
// YouTube's own share button appends by default, and the "&t=" it appends when
// sharing at a timestamp.
//
// Every path here is bounds-checked, so no input can panic.
func ParseVideoID(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrInvalidVideoURL
	}

	// A bare ID, pasted directly.
	if videoIDPattern.MatchString(s) {
		return s, nil
	}

	// url.Parse accepts almost anything, so a scheme-less input like
	// "youtu.be/abc" parses with an empty Host. Prepending a scheme makes the
	// host land where we expect it.
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", ErrInvalidVideoURL
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if !youTubeHosts[host] {
		return "", ErrInvalidVideoURL
	}

	// youtu.be/ID — the ID is the whole first path segment.
	if host == "youtu.be" {
		if id := firstSegment(u.Path); videoIDPattern.MatchString(id) {
			return id, nil
		}
		return "", ErrInvalidVideoURL
	}

	// youtube.com/watch?v=ID — Query() has already stripped &t=, &list=, etc.
	if id := u.Query().Get("v"); id != "" {
		if videoIDPattern.MatchString(id) {
			return id, nil
		}
		return "", ErrInvalidVideoURL
	}

	// youtube.com/{shorts,embed,live,v}/ID
	segments := splitPath(u.Path)
	if len(segments) >= 2 {
		for _, prefix := range pathPrefixes {
			if segments[0] == prefix && videoIDPattern.MatchString(segments[1]) {
				return segments[1], nil
			}
		}
	}

	return "", ErrInvalidVideoURL
}

// ExtractVideoID is the legacy signature, kept so existing callers and tests
// keep compiling. It returns "" where ParseVideoID returns an error.
//
// Deprecated: use ParseVideoID, which distinguishes "not a YouTube URL" from a
// successfully parsed empty result.
func ExtractVideoID(raw string) string {
	id, err := ParseVideoID(raw)
	if err != nil {
		return ""
	}
	return id
}

func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstSegment(p string) string {
	segs := splitPath(p)
	if len(segs) == 0 {
		return ""
	}
	return segs[0]
}
