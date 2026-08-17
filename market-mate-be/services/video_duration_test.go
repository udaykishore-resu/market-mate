package services

import "testing"

// TestParseISO8601Duration covers the format YouTube actually returns, and the
// values it returns for the awkward cases: a zero-length video, a live stream,
// and anything unparseable — all of which must yield 0 rather than a wrong
// number stored on the video row.
func TestParseISO8601Duration(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"minutes and seconds", "PT14M12S", 852},
		{"seconds only", "PT45S", 45},
		{"minutes only", "PT15M", 900},
		{"hours minutes seconds", "PT1H2M3S", 3723},
		{"hours only", "PT2H", 7200},
		{"zero", "PT0S", 0},
		{"empty", "", 0},
		{"no time component", "P1D", 0},
		{"not a duration", "14:12", 0},
		{"trailing digits with no unit", "PT14M12", 0},
		{"garbage after the prefix", "PTxyz", 0},
		{"whitespace is tolerated", "  PT10M  ", 600},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseISO8601Duration(tc.input); got != tc.want {
				t.Errorf("ParseISO8601Duration(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
