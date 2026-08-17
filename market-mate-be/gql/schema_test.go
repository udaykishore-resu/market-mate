package gql

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"market-mate/health"
	"market-mate/models"
	"market-mate/search"
	"market-mate/services"

	"github.com/graphql-go/graphql"
)

type stubSearcher struct {
	results []models.Recipe
	last    search.Query
	calls   int32
}

func (s *stubSearcher) Index(context.Context, models.Recipe) error { return nil }

func (s *stubSearcher) Search(_ context.Context, q search.Query) ([]models.Recipe, error) {
	atomic.AddInt32(&s.calls, 1)
	s.last = q
	return s.results, nil
}

func (s *stubSearcher) Health(context.Context) error { return nil }
func (s *stubSearcher) Kind() string                 { return "elasticsearch" }

type countingStores struct {
	calls int32
}

func (c *countingStores) FindNearbyStores(context.Context, float64, float64) ([]models.Store, error) {
	atomic.AddInt32(&c.calls, 1)
	return []models.Store{
		{Name: "Whole Foods Market", Address: "1 Market Street", Distance: "0.5 km", MapURL: "https://maps.example/1"},
		{Name: "Costco Wholesale", Address: "9 Industrial Parkway", Distance: "3.1 km", MapURL: "https://maps.example/2"},
	}, nil
}

func fixtureRecipe() models.Recipe {
	return models.Recipe{
		VideoID: "dQw4w9WgXcQ",
		Title:   "Perfect Spaghetti Carbonara",
		Channel: "Trattoria Basics",
		Ingredients: []models.Ingredient{
			{Name: "Spaghetti", Quantity: "400 g"},
			{Name: "Guanciale", Quantity: "200 g"},
			{Name: "Egg yolks", Quantity: "4 large"},
			{Name: "Pecorino Romano", Quantity: "100 g"},
			{Name: "Black pepper", Quantity: "2 tsp"},
		},
		Source:       models.SourceFixture,
		Simulated:    true,
		ModelVersion: "fixture@0123456789ab",
		IndexedAt:    time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC),
	}
}

func execute(t *testing.T, cfg Config, query string) map[string]any {
	t.Helper()

	schema, err := New(cfg)
	if err != nil {
		t.Fatalf("building schema: %v", err)
	}
	result := graphql.Do(graphql.Params{Schema: schema, RequestString: query, Context: context.Background()})
	if len(result.Errors) > 0 {
		t.Fatalf("query returned errors: %v", result.Errors)
	}

	raw, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("encoding result: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	return out
}

// TestSearchRecipesCarriesProvenance is the rule the whole repo is built
// around: a fixture recipe that has been through the search index still says so.
func TestSearchRecipesCarriesProvenance(t *testing.T) {
	searcher := &stubSearcher{results: []models.Recipe{fixtureRecipe()}}
	got := execute(t, Config{
		Search:    searcher,
		Simulated: models.Provenance{Video: true, Ingredients: true, Stores: true, Any: true},
	}, `{
		searchRecipes(query: "carbonara", limit: 5) {
			videoId
			title
			modelVersion
			provenance { source simulated video ingredients notice }
		}
	}`)

	recipes := got["searchRecipes"].([]any)
	if len(recipes) != 1 {
		t.Fatalf("got %d recipes, want 1", len(recipes))
	}
	recipe := recipes[0].(map[string]any)
	provenance := recipe["provenance"].(map[string]any)

	if provenance["simulated"] != true {
		t.Error("a fixture recipe was not labelled simulated")
	}
	if provenance["source"] != models.SourceFixture {
		t.Errorf("source = %v, want fixture", provenance["source"])
	}
	if provenance["ingredients"] != true {
		t.Error("an extraction with a fixture model version was not labelled simulated")
	}
	if provenance["notice"] == "" {
		t.Error("a simulated recipe carries no explanatory notice")
	}
	if searcher.last.Text != "carbonara" || searcher.last.Limit != 5 {
		t.Errorf("arguments did not reach the searcher: %+v", searcher.last)
	}
}

func TestLiveRecipeIsNotLabelledSimulated(t *testing.T) {
	live := fixtureRecipe()
	live.Source = models.SourceYouTube
	live.Simulated = false
	live.ModelVersion = "gpt-4o-mini@0123456789ab"

	got := execute(t, Config{Search: &stubSearcher{results: []models.Recipe{live}}},
		`{ searchRecipes { provenance { simulated video ingredients notice } } }`)

	provenance := got["searchRecipes"].([]any)[0].(map[string]any)["provenance"].(map[string]any)
	if provenance["simulated"] != false {
		t.Error("a live recipe was labelled simulated")
	}
	if provenance["notice"] != "" {
		t.Errorf("a live recipe carries a simulation notice: %v", provenance["notice"])
	}
}

// TestIngredientStoresAreBatched is the N+1 guard: a query that asks for shops
// on every ingredient of a recipe must cost one provider call, not one per
// ingredient.
func TestIngredientStoresAreBatched(t *testing.T) {
	provider := &countingStores{}
	recipe := fixtureRecipe()

	got := execute(t, Config{
		Search: &stubSearcher{results: []models.Recipe{recipe}},
		Stores: services.NewStoreLookup(provider, services.NewMemoryCache(time.Minute), time.Minute),
	}, `{
		searchRecipes {
			ingredients {
				name
				stores(lat: 37.7749, lng: -122.4194) { name }
			}
		}
	}`)

	ingredients := got["searchRecipes"].([]any)[0].(map[string]any)["ingredients"].([]any)
	if len(ingredients) != len(recipe.Ingredients) {
		t.Fatalf("got %d ingredients, want %d", len(ingredients), len(recipe.Ingredients))
	}
	for i, raw := range ingredients {
		if stores := raw.(map[string]any)["stores"].([]any); len(stores) != 2 {
			t.Errorf("ingredient %d resolved %d stores, want 2", i, len(stores))
		}
	}
	if calls := atomic.LoadInt32(&provider.calls); calls != 1 {
		t.Errorf("provider called %d times for %d ingredients, want 1", calls, len(recipe.Ingredients))
	}
}

func TestStoresRespectTheRequestedRadius(t *testing.T) {
	got := execute(t, Config{
		Stores:    services.NewStoreLookup(&countingStores{}, services.NewMemoryCache(time.Minute), time.Minute),
		Simulated: models.Provenance{Stores: true, Any: true},
	}, `{
		stores(videoId: "dQw4w9WgXcQ", lat: 37.7749, lng: -122.4194, radiusMeters: 1000) {
			name
			distance
			provenance { simulated source notice }
		}
	}`)

	stores := got["stores"].([]any)
	if len(stores) != 1 {
		t.Fatalf("got %d stores within 1km, want 1 (the 3.1km one must be excluded)", len(stores))
	}
	provenance := stores[0].(map[string]any)["provenance"].(map[string]any)
	if provenance["simulated"] != true {
		t.Error("fixture stores were not labelled simulated")
	}
	if provenance["source"] != models.SourceFixture {
		t.Errorf("source = %v, want fixture", provenance["source"])
	}
}

func TestLiveStoresAreNotLabelled(t *testing.T) {
	got := execute(t, Config{
		Stores:    services.NewStoreLookup(&countingStores{}, services.NewMemoryCache(time.Minute), time.Minute),
		Simulated: models.Provenance{},
	}, `{ stores(videoId: "abc", lat: 0, lng: 0) { provenance { simulated source } } }`)

	provenance := got["stores"].([]any)[0].(map[string]any)["provenance"].(map[string]any)
	if provenance["simulated"] != false {
		t.Error("live stores were labelled simulated")
	}
	if provenance["source"] != models.SourceYouTube {
		t.Errorf("source = %v, want youtube", provenance["source"])
	}
}

// TestRecipeFallsBackToTheSearchIndex covers the deployment with Elasticsearch
// but no Postgres: recipe(videoId:) still answers, and never returns a
// near-miss from a text search.
func TestRecipeFallsBackToTheSearchIndex(t *testing.T) {
	other := fixtureRecipe()
	other.VideoID = "someoneelse"

	cfg := Config{Search: &stubSearcher{results: []models.Recipe{other, fixtureRecipe()}}}
	got := execute(t, cfg, `{ recipe(videoId: "dQw4w9WgXcQ") { videoId title } }`)
	if got["recipe"].(map[string]any)["videoId"] != "dQw4w9WgXcQ" {
		t.Errorf("recipe = %v, want the exact video id", got["recipe"])
	}

	missing := execute(t, cfg, `{ recipe(videoId: "notindexedid") { videoId } }`)
	if missing["recipe"] != nil {
		t.Errorf("recipe = %v, want null for a video that has not been processed", missing["recipe"])
	}
}

// TestEveryDependencyNil is the degradation path: with no store, no searcher and
// no store lookup, the schema still answers rather than erroring.
func TestEveryDependencyNil(t *testing.T) {
	got := execute(t, Config{}, `{
		recipe(videoId: "dQw4w9WgXcQ") { videoId }
		searchRecipes { videoId }
		stores(videoId: "dQw4w9WgXcQ", lat: 0, lng: 0) { name }
		health { status mode checks { name state impl } }
	}`)

	if got["recipe"] != nil {
		t.Errorf("recipe = %v, want null", got["recipe"])
	}
	if recipes := got["searchRecipes"].([]any); len(recipes) != 0 {
		t.Errorf("searchRecipes returned %d results with no backend", len(recipes))
	}
	if stores := got["stores"].([]any); len(stores) != 0 {
		t.Errorf("stores returned %d results with no provider", len(stores))
	}

	healthResult := got["health"].(map[string]any)
	if healthResult["status"] != "ok" {
		t.Errorf("health status = %v, want ok: unconfigured dependencies are not failures", healthResult["status"])
	}
	if len(healthResult["checks"].([]any)) != 3 {
		t.Errorf("health reported %d checks, want 3", len(healthResult["checks"].([]any)))
	}
}

func TestHealthFieldMirrorsTheChecker(t *testing.T) {
	got := execute(t, Config{
		Checker: health.Checker{
			Cache:        services.NewMemoryCache(time.Minute),
			Search:       search.Disabled{},
			Simulated:    models.Provenance{Video: true, Any: true},
			ModelVersion: "fixture@0123456789ab",
		},
	}, `{ health { status service mode modelVersion checks { name ok state impl } } }`)

	h := got["health"].(map[string]any)
	if h["mode"] != "demo" {
		t.Errorf("mode = %v, want demo", h["mode"])
	}
	if h["modelVersion"] != "fixture@0123456789ab" {
		t.Errorf("modelVersion = %v", h["modelVersion"])
	}

	byName := map[string]map[string]any{}
	for _, raw := range h["checks"].([]any) {
		check := raw.(map[string]any)
		byName[check["name"].(string)] = check
	}
	if byName["redis"]["impl"] != "memory" {
		t.Errorf("redis impl = %v, want memory", byName["redis"]["impl"])
	}
	if byName["postgres"]["state"] != "disabled" {
		t.Errorf("postgres state = %v, want disabled", byName["postgres"]["state"])
	}
}

func TestRecipeRequiresAVideoID(t *testing.T) {
	schema, err := New(Config{})
	if err != nil {
		t.Fatalf("building schema: %v", err)
	}
	result := graphql.Do(graphql.Params{Schema: schema, RequestString: `{ recipe(videoId: "   ") { videoId } }`})
	if len(result.Errors) == 0 {
		t.Error("a blank video id should be rejected rather than looked up")
	}
}
