package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"market-mate/models"
)

func TestNewClientRejectsBadConfiguration(t *testing.T) {
	cases := []struct{ name, url, index string }{
		{"empty url", "", "marketmate-recipes"},
		{"no scheme", "elasticsearch:9200", "marketmate-recipes"},
		{"no host", "http://", "marketmate-recipes"},
		{"empty index", "http://elasticsearch:9200", "  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewClient(tc.url, tc.index); err == nil {
				t.Errorf("NewClient(%q, %q) succeeded; a typo must surface at boot", tc.url, tc.index)
			}
		})
	}
}

func TestClientEnsureIndexCreatesOnlyWhenMissing(t *testing.T) {
	var head, put int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			head++
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			put++
			if r.URL.Path != "/marketmate-recipes" {
				t.Errorf("created %q, want /marketmate-recipes", r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("index creation body is not JSON: %v", err)
			}
			if _, ok := body["mappings"]; !ok {
				t.Error("index was created without an explicit mapping")
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"acknowledged":true}`))
		}
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "marketmate-recipes")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if head != 1 || put != 1 {
		t.Errorf("HEAD=%d PUT=%d, want 1 and 1", head, put)
	}
}

func TestClientEnsureIndexToleratesTheCreationRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Another replica created it between our HEAD and our PUT.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"resource_already_exists_exception"}}`))
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL, "marketmate-recipes")
	if err := client.EnsureIndex(context.Background()); err != nil {
		t.Errorf("EnsureIndex: %v, want the existing index to be accepted", err)
	}
}

func TestClientIndexPreservesProvenance(t *testing.T) {
	var received document
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/marketmate-recipes/_doc/dQw4w9WgXcQ" {
			t.Errorf("indexed at %q, want the document keyed by video id", got)
		}
		if got := r.URL.Query().Get("refresh"); got != "wait_for" {
			t.Errorf("refresh = %q, want wait_for so a fresh recipe is searchable", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decoding indexed document: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":"created"}`))
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL, "marketmate-recipes")
	err := client.Index(context.Background(), models.Recipe{
		VideoID:      "dQw4w9WgXcQ",
		Title:        "Perfect Spaghetti Carbonara",
		Channel:      "Trattoria Basics",
		Ingredients:  []models.Ingredient{{Name: "Spaghetti", Quantity: "400 g"}},
		Source:       models.SourceFixture,
		Simulated:    true,
		ModelVersion: "fixture@abc123",
	})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	if !received.Simulated || received.Source != models.SourceFixture {
		t.Errorf("provenance was lost on the way into the index: source=%q simulated=%v",
			received.Source, received.Simulated)
	}
	if received.IndexedAt.IsZero() {
		t.Error("indexed_at was not set")
	}
	if len(received.Ingredients) != 1 || received.Ingredients[0].Name != "Spaghetti" {
		t.Errorf("ingredients = %+v", received.Ingredients)
	}
}

func TestClientIndexRejectsEmptyID(t *testing.T) {
	client, _ := NewClient("http://127.0.0.1:1", "marketmate-recipes")
	if err := client.Index(context.Background(), models.Recipe{}); err == nil {
		t.Error("indexing a recipe with no video id should fail rather than write a junk document")
	}
}

func TestClientSearchDecodesHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/marketmate-recipes/_search" {
			t.Errorf("searched %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"hits":{"hits":[
			{"_source":{"video_id":"abc","title":"Carbonara","channel":"Trattoria",
			 "ingredients":[{"name":"Spaghetti","quantity":"400 g","unit":""}],
			 "source":"fixture","simulated":true,"indexed_at":"2026-08-15T10:00:00Z"}},
			{"_source":{"video_id":"def","title":"Cookies","source":"youtube","simulated":false,
			 "indexed_at":"2026-08-15T11:00:00Z"}}
		]}}`))
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL, "marketmate-recipes")
	got, err := client.Search(context.Background(), Query{Text: "carbonara"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hits, want 2", len(got))
	}
	if !got[0].Simulated {
		t.Error("a fixture document came back from the index without its provenance")
	}
	if got[1].Simulated {
		t.Error("a live document was reported as simulated")
	}
	if got[0].Ingredients[0].Quantity != "400 g" {
		t.Errorf("ingredient quantity = %q", got[0].Ingredients[0].Quantity)
	}
}

// TestClientSearchTreatsMissingIndexAsEmpty keeps a fresh deployment from
// reporting an outage before anything has been indexed.
func TestClientSearchTreatsMissingIndexAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"type":"index_not_found_exception"}}`))
	}))
	defer srv.Close()

	client, _ := NewClient(srv.URL, "marketmate-recipes")
	got, err := client.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d hits from a missing index", len(got))
	}
}

func TestClientHealth(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"green is healthy", "green", false},
		// Single-node development clusters are permanently yellow.
		{"yellow is healthy", "yellow", false},
		{"red is not", "red", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"status":"` + tc.status + `"}`))
			}))
			defer srv.Close()

			client, _ := NewClient(srv.URL, "marketmate-recipes")
			err := client.Health(context.Background())
			if (err != nil) != tc.wantErr {
				t.Errorf("Health = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestClientHealthReportsAnUnreachableCluster(t *testing.T) {
	client, _ := NewClient("http://127.0.0.1:1", "marketmate-recipes")
	if err := client.Health(context.Background()); err == nil {
		t.Error("an unreachable cluster reported itself healthy")
	}
}

// --- degraded backends ---------------------------------------------------------

func TestDisabledSearcher(t *testing.T) {
	var s Searcher = Disabled{}
	ctx := context.Background()

	got, err := s.Search(ctx, Query{Text: "anything"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Error("Search returned nil; the endpoint encodes this straight to JSON and nil is null")
	}
	if len(got) != 0 {
		t.Errorf("got %d results from a disabled searcher", len(got))
	}
	if err := s.Index(ctx, models.Recipe{VideoID: "abc"}); err != nil {
		t.Errorf("Index on a disabled searcher: %v", err)
	}
	if err := s.Health(ctx); err != nil {
		t.Errorf("Health on a disabled searcher: %v", err)
	}
	if s.Kind() != "disabled" {
		t.Errorf("Kind = %q, want disabled", s.Kind())
	}
}

type stubRecipeSource struct {
	query       string
	ingredients []string
	limit       int
	err         error
}

func (s *stubRecipeSource) SearchRecipes(_ context.Context, query string, ingredients []string, limit int) ([]models.Recipe, error) {
	s.query, s.ingredients, s.limit = query, ingredients, limit
	if s.err != nil {
		return nil, s.err
	}
	return []models.Recipe{{VideoID: "abc", Source: models.SourceFixture, Simulated: true}}, nil
}

func (s *stubRecipeSource) Ping(context.Context) error { return s.err }

func TestDBSearcherPassesTheNormalisedQuery(t *testing.T) {
	source := &stubRecipeSource{}
	s := NewDBSearcher(source)

	got, err := s.Search(context.Background(), Query{Text: "  Pasta ", Ingredients: []string{" EGG ", ""}, Limit: 0})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if source.query != "Pasta" {
		t.Errorf("query = %q, want the trimmed text", source.query)
	}
	if len(source.ingredients) != 1 || source.ingredients[0] != "egg" {
		t.Errorf("ingredients = %v, want [egg]", source.ingredients)
	}
	if source.limit != DefaultLimit {
		t.Errorf("limit = %d, want the default %d", source.limit, DefaultLimit)
	}
	if len(got) != 1 || !got[0].Simulated {
		t.Errorf("results lost their provenance: %+v", got)
	}
	if s.Kind() != "postgres" {
		t.Errorf("Kind = %q, want postgres", s.Kind())
	}
}

func TestDBSearcherReportsStoreFailures(t *testing.T) {
	s := NewDBSearcher(&stubRecipeSource{err: errors.New("connection refused")})
	if _, err := s.Search(context.Background(), Query{}); err == nil {
		t.Error("a failing store must surface as a search error, not as an empty result")
	}
	if err := s.Health(context.Background()); err == nil {
		t.Error("Health did not report the failing store")
	}
}
