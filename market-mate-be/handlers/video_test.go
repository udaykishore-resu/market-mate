package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"market-mate/models"
	"market-mate/services"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// --- Test doubles ------------------------------------------------------------

// countingVideo wraps the fixture provider and counts calls, so cache-hit tests
// can assert that zero providers were touched.
type countingVideo struct {
	inner services.VideoProvider
	calls int32
	err   error
}

func (c *countingVideo) GetVideoDetails(ctx context.Context, id string) (*services.VideoDetails, error) {
	atomic.AddInt32(&c.calls, 1)
	if c.err != nil {
		return nil, c.err
	}
	return c.inner.GetVideoDetails(ctx, id)
}

type countingIngredients struct {
	inner services.IngredientProvider
	calls int32
	err   error
}

func (c *countingIngredients) ExtractIngredients(ctx context.Context, d string) ([]models.Ingredient, error) {
	atomic.AddInt32(&c.calls, 1)
	if c.err != nil {
		return nil, c.err
	}
	return c.inner.ExtractIngredients(ctx, d)
}

type countingStores struct {
	inner services.StoreProvider
	calls int32
	err   error
}

func (c *countingStores) FindNearbyStores(ctx context.Context, lat, lng float64) ([]models.Store, error) {
	atomic.AddInt32(&c.calls, 1)
	if c.err != nil {
		return nil, c.err
	}
	return c.inner.FindNearbyStores(ctx, lat, lng)
}

// stubLocation returns a fixed coordinate without any network call, so tests
// are hermetic and location behaviour is directly controllable.
type stubLocation struct {
	loc      services.Location
	resolved bool
}

func (s stubLocation) Resolve(context.Context, string) (services.Location, bool) {
	return s.loc, s.resolved
}

type harness struct {
	router      *gin.Engine
	video       *countingVideo
	ingredients *countingIngredients
	stores      *countingStores
}

func newHarness(t *testing.T, opts ...func(*VideoHandlerConfig)) *harness {
	t.Helper()

	h := &harness{
		video:       &countingVideo{inner: services.NewFixtureVideoProvider()},
		ingredients: &countingIngredients{inner: services.NewFixtureIngredientProvider()},
		stores:      &countingStores{inner: services.NewFixtureStoreProvider()},
	}

	cfg := VideoHandlerConfig{
		VideoService:        h.video,
		StoreFinder:         h.stores,
		IngredientExtractor: h.ingredients,
		CacheService:        services.NewCacheService(),
		LocationService:     stubLocation{loc: services.DefaultLocation, resolved: false},
		Simulated:           models.Provenance{Video: true, Ingredients: true, Stores: true, Any: true},
	}
	for _, o := range opts {
		o(&cfg)
	}

	handler := NewVideoHandler(cfg)
	r := gin.New()
	r.POST("/api/process-video", handler.ProcessVideo)
	r.GET("/api/health", handler.Health)
	h.router = r
	return h
}

func (h *harness) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/process-video", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func decodeRecipe(t *testing.T, rec *httptest.ResponseRecorder) models.RecipeResponse {
	t.Helper()
	var r models.RecipeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decoding response %q: %v", rec.Body.String(), err)
	}
	return r
}

// --- Tests -------------------------------------------------------------------

// TestProcessVideo_NoKeysRequired is User Story 1: the whole pipeline runs and
// returns a usable result with no API keys and no network.
func TestProcessVideo_NoKeysRequired(t *testing.T) {
	h := newHarness(t)
	rec := h.post(t, `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got := decodeRecipe(t, rec)

	if len(got.Ingredients) == 0 {
		t.Error("expected a non-empty ingredient list")
	}
	if len(got.Stores) == 0 {
		t.Error("expected a non-empty store list")
	}
	for i, ing := range got.Ingredients {
		if strings.TrimSpace(ing.Name) == "" {
			t.Errorf("ingredient %d has an empty name", i)
		}
		if strings.TrimSpace(ing.Quantity) == "" {
			t.Errorf("ingredient %d (%s) has an empty quantity", i, ing.Name)
		}
	}
	for i, s := range got.Stores {
		if s.Name == "" || s.Address == "" || s.MapURL == "" {
			t.Errorf("store %d is incomplete: %+v", i, s)
		}
	}
	if got.Video == nil || got.Video.Title == "" {
		t.Error("expected video metadata on the response")
	}
}

// TestProcessVideo_LabelsSimulatedData is FR-003: a fixture response must never
// pass for a live one.
func TestProcessVideo_LabelsSimulatedData(t *testing.T) {
	h := newHarness(t)
	got := decodeRecipe(t, h.post(t, `{"url":"dQw4w9WgXcQ"}`))

	if !got.Simulated.Any {
		t.Error("response from fixture providers is not flagged as simulated")
	}
	if !strings.Contains(strings.ToLower(got.Notice), "simulated") {
		t.Errorf("expected the notice to say results are simulated, got %q", got.Notice)
	}
	for _, want := range []string{"video details", "ingredient extraction", "store search"} {
		if !strings.Contains(strings.ToLower(got.Notice), want) {
			t.Errorf("notice does not name %q as simulated: %q", want, got.Notice)
		}
	}
}

func TestProcessVideo_LiveProvidersAreNotLabelled(t *testing.T) {
	h := newHarness(t, func(c *VideoHandlerConfig) {
		c.Simulated = models.Provenance{}
	})
	got := decodeRecipe(t, h.post(t, `{"url":"dQw4w9WgXcQ"}`))

	if got.Simulated.Any {
		t.Error("live providers should not be flagged as simulated")
	}
	if strings.Contains(strings.ToLower(got.Notice), "simulated") {
		t.Errorf("live response should not carry a simulation notice, got %q", got.Notice)
	}
}

// TestProcessVideo_RejectsBadURLs is User Story 2 at the HTTP boundary. The two
// panic inputs are included: before the parser rewrite these took the goroutine
// down rather than returning a status code.
func TestProcessVideo_RejectsBadURLs(t *testing.T) {
	cases := []struct{ name, body string }{
		{"empty url", `{"url":""}`},
		{"short string (previously panicked)", `{"url":"short"}`},
		{"whitespace", `{"url":"   "}`},
		{"non-youtube", `{"url":"https://vimeo.com/12345"}`},
		{"plain text", `{"url":"how do I make pasta"}`},
		{"missing field", `{}`},
		{"malformed json", `{"url":`},
		{"wrong type", `{"url":123}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.post(t, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400. body: %s", rec.Code, rec.Body)
			}
			var e models.ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
				t.Fatalf("error body is not the documented shape: %s", rec.Body)
			}
			if e.Error == "" {
				t.Error("error response carries no message")
			}
			if atomic.LoadInt32(&h.video.calls) != 0 {
				t.Error("an invalid URL reached the video provider")
			}
		})
	}
}

// TestProcessVideo_AcceptsEveryURLForm confirms the share-button formats work
// end to end, not just in the parser's own unit test.
func TestProcessVideo_AcceptsEveryURLForm(t *testing.T) {
	urls := []string{
		"dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ?si=Ab1Cd2Ef3Gh4",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=30s",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			h := newHarness(t)
			body, _ := json.Marshal(map[string]string{"url": u})
			rec := h.post(t, string(body))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d for %q: %s", rec.Code, u, rec.Body)
			}
			got := decodeRecipe(t, rec)
			// Every form points at the same video, so all must resolve to the
			// same fixture recipe.
			if got.Video.ID != "dQw4w9WgXcQ" {
				t.Errorf("resolved video ID = %q, want dQw4w9WgXcQ", got.Video.ID)
			}
		})
	}
}

// TestProcessVideo_UsesResolvedLocation is User Story 3: the store search must
// follow the client, not a constant. The old handler passed a literal
// 37.7749/-122.4194 to every request on earth.
func TestProcessVideo_UsesResolvedLocation(t *testing.T) {
	berlin := services.Location{Latitude: 52.5200, Longitude: 13.4050, Label: "Berlin"}
	h := newHarness(t, func(c *VideoHandlerConfig) {
		c.LocationService = stubLocation{loc: berlin, resolved: true}
	})
	got := decodeRecipe(t, h.post(t, `{"url":"dQw4w9WgXcQ"}`))

	if got.Location.Latitude != berlin.Latitude || got.Location.Longitude != berlin.Longitude {
		t.Errorf("search location = %.4f,%.4f, want %.4f,%.4f",
			got.Location.Latitude, got.Location.Longitude, berlin.Latitude, berlin.Longitude)
	}
	if got.Location.Estimated {
		t.Error("a successfully resolved location must not be marked estimated")
	}
	if got.Location.Label != "Berlin" {
		t.Errorf("location label = %q, want Berlin", got.Location.Label)
	}

	// The stores must actually be near Berlin, not near the old SF constant.
	if len(got.Stores) == 0 {
		t.Fatal("no stores returned")
	}
	if !strings.Contains(got.Stores[0].MapURL, "52.5") {
		t.Errorf("store map URL is not near the search location: %s", got.Stores[0].MapURL)
	}
}

func TestProcessVideo_MarksFallbackLocationEstimated(t *testing.T) {
	h := newHarness(t) // default harness: resolved=false
	got := decodeRecipe(t, h.post(t, `{"url":"dQw4w9WgXcQ"}`))

	if !got.Location.Estimated {
		t.Error("an unresolved location must be marked estimated so the UI can say so")
	}
}

// TestProcessVideo_CacheHitSkipsProviders is User Story 4 / SC-004.
func TestProcessVideo_CacheHitSkipsProviders(t *testing.T) {
	h := newHarness(t)
	body := `{"url":"https://youtu.be/dQw4w9WgXcQ"}`

	first := decodeRecipe(t, h.post(t, body))
	if first.Cached {
		t.Error("the first request should not be served from cache")
	}
	callsAfterFirst := atomic.LoadInt32(&h.video.calls)
	if callsAfterFirst != 1 {
		t.Fatalf("video provider called %d times on the first request, want 1", callsAfterFirst)
	}

	second := decodeRecipe(t, h.post(t, body))
	if !second.Cached {
		t.Error("the second identical request should be served from cache")
	}
	if got := atomic.LoadInt32(&h.video.calls); got != 1 {
		t.Errorf("video provider called %d times total; a cache hit must call no providers", got)
	}
	if got := atomic.LoadInt32(&h.ingredients.calls); got != 1 {
		t.Errorf("ingredient provider called %d times total, want 1", got)
	}
	if got := atomic.LoadInt32(&h.stores.calls); got != 1 {
		t.Errorf("store provider called %d times total, want 1", got)
	}

	if len(second.Ingredients) != len(first.Ingredients) {
		t.Error("the cached response differs from the original")
	}
}

func TestProcessVideo_DifferentLocationsCacheSeparately(t *testing.T) {
	loc := &stubLocation{loc: services.DefaultLocation, resolved: true}
	h := newHarness(t, func(c *VideoHandlerConfig) { c.LocationService = loc })

	h.post(t, `{"url":"dQw4w9WgXcQ"}`)
	loc.loc = services.Location{Latitude: 40.7128, Longitude: -74.0060, Label: "New York"}
	got := decodeRecipe(t, h.post(t, `{"url":"dQw4w9WgXcQ"}`))

	if got.Cached {
		t.Error("a different search location must not hit the cache entry for another location")
	}
	if got.Location.Label != "New York" {
		t.Errorf("location = %q, want New York", got.Location.Label)
	}
}

// --- Provider failure handling ----------------------------------------------

func TestProcessVideo_ProviderFailuresAreReported(t *testing.T) {
	boom := errors.New("upstream exploded")

	t.Run("video provider fails", func(t *testing.T) {
		h := newHarness(t)
		h.video.err = boom
		rec := h.post(t, `{"url":"dQw4w9WgXcQ"}`)

		if rec.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", rec.Code)
		}
		var e models.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Stage != "video" {
			t.Errorf("stage = %q, want video", e.Stage)
		}
		if strings.Contains(rec.Body.String(), "upstream exploded") {
			t.Error("internal error text leaked to the client")
		}
	})

	t.Run("ingredient provider fails", func(t *testing.T) {
		h := newHarness(t)
		h.ingredients.err = boom
		rec := h.post(t, `{"url":"dQw4w9WgXcQ"}`)

		if rec.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", rec.Code)
		}
		var e models.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Stage != "ingredients" {
			t.Errorf("stage = %q, want ingredients", e.Stage)
		}
	})

	t.Run("store provider fails", func(t *testing.T) {
		h := newHarness(t)
		h.stores.err = boom
		rec := h.post(t, `{"url":"dQw4w9WgXcQ"}`)

		if rec.Code != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", rec.Code)
		}
		var e models.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Stage != "stores" {
			t.Errorf("stage = %q, want stores", e.Stage)
		}
	})
}

// TestProcessVideo_JSONArraysAreNeverNull guards the frontend, which maps over
// both lists directly: a nil Go slice encodes as `null` and would crash it.
func TestProcessVideo_JSONArraysAreNeverNull(t *testing.T) {
	h := newHarness(t)
	body := h.post(t, `{"url":"dQw4w9WgXcQ"}`).Body.String()

	if strings.Contains(body, `"ingredients":null`) {
		t.Error("ingredients serialised as null; must be []")
	}
	if strings.Contains(body, `"stores":null`) {
		t.Error("stores serialised as null; must be []")
	}
}

// --- Health ------------------------------------------------------------------

func TestHealth_ReportsProviderModes(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health body is not JSON: %s", rec.Body)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["mode"] != "demo" {
		t.Errorf("mode = %v, want demo (all providers are fixtures here)", body["mode"])
	}
	providers, ok := body["providers"].(map[string]any)
	if !ok {
		t.Fatal("health response has no providers block")
	}
	for _, k := range []string{"video", "ingredients", "stores"} {
		if providers[k] != "simulated" {
			t.Errorf("provider %s = %v, want simulated", k, providers[k])
		}
	}
}

func TestHealth_ReportsLiveMode(t *testing.T) {
	h := newHarness(t, func(c *VideoHandlerConfig) { c.Simulated = models.Provenance{} })
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["mode"] != "live" {
		t.Errorf("mode = %v, want live", body["mode"])
	}
}
