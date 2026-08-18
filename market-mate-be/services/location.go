package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DefaultLocation is used when the client IP cannot be resolved. It is
// documented and surfaced to the user as an estimate, rather than silently
// pretending to be their location — which is what the hardcoded
// 37.7749/-122.4194 in the old handler did to every user on earth.
var DefaultLocation = Location{
	Latitude:  37.7749,
	Longitude: -122.4194,
	Label:     "San Francisco, CA (default)",
}

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Label     string  `json:"label,omitempty"`
}

// LocationService resolves a client IP to a coordinate via ipapi.co.
type LocationService struct {
	client  *http.Client
	baseURL string
}

func NewLocationService() *LocationService {
	return &LocationService{
		// A bounded client: without this timeout a hung geo-IP lookup holds the
		// request open indefinitely.
		client:  &http.Client{Timeout: 2 * time.Second},
		baseURL: "https://ipapi.co",
	}
}

// Resolve returns coordinates for an IP. The bool reports whether this is a
// genuine resolution (true) or the documented fallback (false).
//
// Private, loopback, and unspecified addresses short-circuit without a network
// call: a geo-IP service cannot say anything useful about 127.0.0.1, so asking
// costs a round trip on every local request and returns nothing.
func (ls *LocationService) Resolve(ctx context.Context, ipStr string) (Location, bool) {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return DefaultLocation, false
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/%s/json/", ls.baseURL, ipStr), nil)
	if err != nil {
		return DefaultLocation, false
	}

	resp, err := ls.client.Do(req)
	if err != nil {
		return DefaultLocation, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return DefaultLocation, false
	}

	var result struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		City      string  `json:"city"`
		Region    string  `json:"region"`
		Error     bool    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return DefaultLocation, false
	}
	// ipapi.co answers 200 with {"error": true} for reserved ranges, and 0,0 is
	// a point in the Atlantic — neither is worth searching around.
	if result.Error || (result.Latitude == 0 && result.Longitude == 0) {
		return DefaultLocation, false
	}

	label := result.City
	if result.Region != "" {
		if label != "" {
			label += ", "
		}
		label += result.Region
	}
	return Location{Latitude: result.Latitude, Longitude: result.Longitude, Label: label}, true
}

// GetLocationFromIP is the previous signature, retained for compatibility.
//
// Deprecated: use Resolve, which bounds the request with a context and reports
// whether the result is real or a fallback.
func (ls *LocationService) GetLocationFromIP(ip string) (*Location, error) {
	loc, ok := ls.Resolve(context.Background(), ip)
	if !ok {
		return &loc, fmt.Errorf("could not resolve location for %q; using default", ip)
	}
	return &loc, nil
}
