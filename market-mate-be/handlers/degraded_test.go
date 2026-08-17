package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"market-mate/models"
	"market-mate/search"
	"market-mate/services"

	"github.com/gin-gonic/gin"
)

// The contract makes every dependency opt-in, which is only true if the service
// behaves with all of them absent. These tests run the handler with Postgres,
// Redis and Elasticsearch all unconfigured — the state of a fresh clone, and the
// state of CI.

func newDegradedRouter(t *testing.T, opts ...func(*VideoHandlerConfig)) *gin.Engine {
	t.Helper()

	cfg := VideoHandlerConfig{
		VideoService:        services.NewFixtureVideoProvider(),
		StoreFinder:         services.NewFixtureStoreProvider(),
		IngredientExtractor: services.NewFixtureIngredientProvider(),
		LocationService:     stubLocation{loc: services.DefaultLocation},
		// Store, Search, CacheService and StoreLookup are deliberately nil.
		Simulated: models.Provenance{Video: true, Ingredients: true, Stores: true, Any: true},
	}
	for _, o := range opts {
		o(&cfg)
	}

	h := NewVideoHandler(cfg)
	r := gin.New()
	r.POST("/api/process-video", h.ProcessVideo)
	r.GET("/api/health", h.Health)
	r.GET("/api/recipes/search", h.SearchRecipes)
	r.POST("/api/admin/reindex", h.Reindex)
	return r
}

func do(t *testing.T, r *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestPipelineAnswersWithEveryDependencyAbsent(t *testing.T) {
	r := newDegradedRouter(t)
	rec := do(t, r, http.MethodPost, "/api/process-video", `{"url":"dQw4w9WgXcQ"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got models.RecipeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got.Ingredients) == 0 || len(got.Stores) == 0 {
		t.Error("the pipeline returned an empty result with no infrastructure")
	}
	if !got.Simulated.Any {
		t.Error("fixture data lost its provenance label on the degraded path")
	}
}

func TestSearchAnswersWithNoBackend(t *testing.T) {
	r := newDegradedRouter(t)
	rec := do(t, r, http.MethodGet, "/api/recipes/search?q=carbonara", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got models.SearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got.Backend != "disabled" {
		t.Errorf("backend = %q, want disabled", got.Backend)
	}
	if got.Results == nil {
		t.Error("results serialised as null; the frontend maps over this array")
	}
	if got.Notice == "" {
		t.Error("an empty result from a disabled backend must say so, not look like no matches")
	}
	if strings.Contains(rec.Body.String(), `"results":null`) {
		t.Error("results serialised as null")
	}
}

func TestSearchRejectsABadLimit(t *testing.T) {
	r := newDegradedRouter(t)
	for _, limit := range []string{"abc", "-1", "1.5"} {
		rec := do(t, r, http.MethodGet, "/api/recipes/search?limit="+limit, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want 400", limit, rec.Code)
		}
	}
}

func TestReindexWithoutPostgres(t *testing.T) {
	r := newDegradedRouter(t)
	rec := do(t, r, http.MethodPost, "/api/admin/reindex", "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
	}
	var e models.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !strings.Contains(e.Error, "MM_POSTGRES_DSN") {
		t.Errorf("the error should name the variable to set, got %q", e.Error)
	}
}

func TestHealthReportsEveryDependencyDisabled(t *testing.T) {
	r := newDegradedRouter(t)
	rec := do(t, r, http.MethodGet, "/api/health", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: unconfigured optional dependencies are not a failure", rec.Code)
	}

	var body struct {
		Status string `json:"status"`
		Checks struct {
			Postgres      map[string]any    `json:"postgres"`
			Redis         map[string]any    `json:"redis"`
			Elasticsearch map[string]any    `json:"elasticsearch"`
			Providers     map[string]string `json:"providers"`
		} `json:"checks"`
		ModelVersion string `json:"model_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	for name, check := range map[string]map[string]any{
		"postgres":      body.Checks.Postgres,
		"redis":         body.Checks.Redis,
		"elasticsearch": body.Checks.Elasticsearch,
	} {
		if check["ok"] != true {
			t.Errorf("%s check ok = %v, want true", name, check["ok"])
		}
		if check["state"] != "disabled" {
			t.Errorf("%s state = %v, want disabled", name, check["state"])
		}
	}
	if body.Checks.Redis["impl"] != "memory" {
		t.Errorf("redis impl = %v, want memory: the health report must name what actually resolved",
			body.Checks.Redis["impl"])
	}
	if body.Checks.Providers["transcript"] != "fixture" {
		t.Errorf("transcript provider = %v, want fixture", body.Checks.Providers["transcript"])
	}
	if !strings.HasPrefix(body.ModelVersion, "fixture@") {
		t.Errorf("model_version = %q, want a fixture version", body.ModelVersion)
	}
}

func TestHealthReportsDegradedWhenAConfiguredDependencyIsDown(t *testing.T) {
	r := newDegradedRouter(t, func(c *VideoHandlerConfig) {
		c.Search = failingSearcher{}
	})
	rec := do(t, r, http.MethodGet, "/api/health", "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: a dead search backend degrades the service, it does not stop it", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
		Checks struct {
			Elasticsearch map[string]any `json:"elasticsearch"`
		} `json:"checks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if body.Status != "degraded" {
		t.Errorf("status = %q, want degraded", body.Status)
	}
	if body.Checks.Elasticsearch["state"] != "down" {
		t.Errorf("elasticsearch state = %v, want down", body.Checks.Elasticsearch["state"])
	}
	if body.Checks.Elasticsearch["error"] == "" {
		t.Error("a down dependency should say why")
	}
}

func TestHealthReports503WhileDraining(t *testing.T) {
	ready := &atomic.Bool{}
	ready.Store(false)
	r := newDegradedRouter(t, func(c *VideoHandlerConfig) { c.Ready = ready })

	rec := do(t, r, http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 while draining", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "shutting_down") {
		t.Errorf("body should say the service is shutting down: %s", rec.Body)
	}
}

// TestIndexingFailureDoesNotFailTheRequest: the search index is derived data.
// Losing a write to it costs a search hit, and failing the user's request over
// it would cost the whole answer.
func TestIndexingFailureDoesNotFailTheRequest(t *testing.T) {
	failing := &failingIndexer{}
	r := newDegradedRouter(t, func(c *VideoHandlerConfig) { c.Search = failing })

	rec := do(t, r, http.MethodPost, "/api/process-video", `{"url":"dQw4w9WgXcQ"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 despite the index write failing: %s", rec.Code, rec.Body)
	}
	if atomic.LoadInt32(&failing.calls) == 0 {
		t.Error("a successful extraction was not offered to the index")
	}
}

type failingSearcher struct{}

func (failingSearcher) Index(context.Context, models.Recipe) error { return nil }
func (failingSearcher) Search(context.Context, search.Query) ([]models.Recipe, error) {
	return nil, errors.New("connection refused")
}
func (failingSearcher) Health(context.Context) error { return errors.New("connection refused") }
func (failingSearcher) Kind() string                 { return "elasticsearch" }

type failingIndexer struct {
	failingSearcher
	calls int32
}

func (f *failingIndexer) Index(context.Context, models.Recipe) error {
	atomic.AddInt32(&f.calls, 1)
	return errors.New("index is read-only")
}

// TestSearchReportsBackendFailure is the other half: a configured backend that
// errors must not be reported as "no matches".
func TestSearchReportsBackendFailure(t *testing.T) {
	r := newDegradedRouter(t, func(c *VideoHandlerConfig) { c.Search = failingSearcher{} })
	rec := do(t, r, http.MethodGet, "/api/recipes/search?q=pasta", "")

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Error("internal error text leaked to the client")
	}
}
