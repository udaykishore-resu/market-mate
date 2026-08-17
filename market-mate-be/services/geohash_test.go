package services

import "testing"

// TestGeohash checks the encoder against published reference values rather than
// against itself: "ezs42" and "u4pruydqqvj" are the two examples in the original
// geohash description, so a subtle bit-order mistake fails here instead of
// quietly sharding the cache.
func TestGeohash(t *testing.T) {
	cases := []struct {
		name      string
		lat, lng  float64
		precision int
		want      string
	}{
		{"reference ezs42", 42.6, -5.6, 5, "ezs42"},
		{"reference u4pruydqqvj", 57.64911, 10.40744, 11, "u4pruydqqvj"},
		{"null island", 0, 0, 5, "s0000"},
		{"south west corner", -90, -180, 5, "00000"},
		{"north east corner", 90, 180, 5, "zzzzz"},
		{"san francisco", 37.7749, -122.4194, 5, "9q8yy"},
		{"london", 51.5074, -0.1278, 5, "gcpvj"},
		{"precision 1", 42.6, -5.6, 1, "e"},
		{"precision 0 is empty", 42.6, -5.6, 0, ""},
		{"negative precision is empty", 42.6, -5.6, -3, ""},

		// Out-of-range input is clamped, not rejected: a geo-IP provider that
		// returns 90.0000001 should not cost us a cache key.
		{"latitude above pole clamps", 91, 180, 5, "zzzzz"},
		{"longitude below limit clamps", -90, -181, 5, "00000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Geohash(tc.lat, tc.lng, tc.precision); got != tc.want {
				t.Errorf("Geohash(%v, %v, %d) = %q, want %q", tc.lat, tc.lng, tc.precision, got, tc.want)
			}
		})
	}
}

// TestGeohashPrefixIsStable is the property the cache relies on: a shorter
// geohash is a prefix of a longer one for the same point, so precision 5 is a
// genuine 5km cell and not an unrelated string.
func TestGeohashPrefixIsStable(t *testing.T) {
	const lat, lng = 48.8584, 2.2945
	long := Geohash(lat, lng, 9)
	for p := 1; p <= 9; p++ {
		if got := Geohash(lat, lng, p); got != long[:p] {
			t.Errorf("Geohash precision %d = %q, want prefix %q of %q", p, got, long[:p], long)
		}
	}
}

// TestGeohashGroupsNearbyPoints is why this replaced coordinate rounding: two
// points 20 metres apart shared no cache entry when the old key rounded to two
// decimals across a boundary.
func TestGeohashGroupsNearbyPoints(t *testing.T) {
	a := Geohash(37.7749, -122.4194, GeohashPrecision)
	b := Geohash(37.7751, -122.4196, GeohashPrecision)
	if a != b {
		t.Errorf("two points 25m apart hashed to %q and %q; they should share a cell", a, b)
	}

	far := Geohash(40.7128, -74.0060, GeohashPrecision)
	if far == a {
		t.Errorf("San Francisco and New York both hashed to %q", far)
	}
}

func TestCacheKeys(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "result key carries the namespace, video and cell",
			got:  ResultKey("dQw4w9WgXcQ", 37.7749, -122.4194),
			want: "mm:v1:result:dQw4w9WgXcQ:9q8yy",
		},
		{
			name: "store key is separate from the result key",
			got:  StoreKey("dQw4w9WgXcQ", 37.7749, -122.4194),
			want: "mm:v1:stores:dQw4w9WgXcQ:9q8yy",
		},
		{
			name: "ingredient lookups share one cell-wide entry",
			got:  StoreKey("", 37.7749, -122.4194),
			want: "mm:v1:stores::9q8yy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("key = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestResultKeySeparatesVideosAndCells(t *testing.T) {
	base := ResultKey("aaaaaaaaaaa", 37.7749, -122.4194)

	if same := ResultKey("aaaaaaaaaaa", 37.7751, -122.4196); same != base {
		t.Errorf("nearby coordinates produced different keys: %q vs %q", base, same)
	}
	if other := ResultKey("bbbbbbbbbbb", 37.7749, -122.4194); other == base {
		t.Error("two different videos share a cache key")
	}
	if far := ResultKey("aaaaaaaaaaa", 40.7128, -74.0060); far == base {
		t.Error("two different cities share a cache key")
	}
}
