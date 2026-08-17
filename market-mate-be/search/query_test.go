package search

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestBuildSearchBody asserts the exact JSON sent to Elasticsearch.
//
// A wrong field name or a missing nested wrapper does not fail: it returns no
// hits, which at the API boundary is indistinguishable from an empty index.
// This test is the only place that difference is visible without a cluster.
func TestBuildSearchBody(t *testing.T) {
	cases := []struct {
		name  string
		query Query
		want  string
	}{
		{
			name:  "no query lists the index by recency",
			query: Query{},
			want: `{
				"size": 20,
				"_source": true,
				"query": {"match_all": {}},
				"sort": [{"indexed_at": {"order": "desc"}}]
			}`,
		},
		{
			name:  "free text spans the title and the nested ingredient names",
			query: Query{Text: "carbonara", Limit: 5},
			want: `{
				"size": 5,
				"_source": true,
				"query": {"bool": {"must": [
					{"bool": {
						"minimum_should_match": 1,
						"should": [
							{"multi_match": {"query": "carbonara", "fields": ["title^3", "channel"]}},
							{"nested": {
								"path": "ingredients",
								"query": {"multi_match": {"query": "carbonara", "fields": ["ingredients.name"]}}
							}}
						]
					}}
				]}}
			}`,
		},
		{
			name:  "one ingredient is a term filter",
			query: Query{Ingredients: []string{"Spaghetti"}, Limit: 3},
			want: `{
				"size": 3,
				"_source": true,
				"query": {"bool": {"filter": [
					{"nested": {
						"path": "ingredients",
						"query": {"term": {"ingredients.name.keyword": "spaghetti"}}
					}}
				]}}
			}`,
		},
		{
			name:  "a repeated ingredient becomes a terms filter",
			query: Query{Ingredients: []string{"Spaghetti", "Guanciale", "  Pecorino  "}, Limit: 3},
			want: `{
				"size": 3,
				"_source": true,
				"query": {"bool": {"filter": [
					{"nested": {
						"path": "ingredients",
						"query": {"terms": {"ingredients.name.keyword": ["spaghetti", "guanciale", "pecorino"]}}
					}}
				]}}
			}`,
		},
		{
			name:  "text and ingredients combine as must plus filter",
			query: Query{Text: "pasta", Ingredients: []string{"egg"}, Limit: 2},
			want: `{
				"size": 2,
				"_source": true,
				"query": {"bool": {
					"must": [
						{"bool": {
							"minimum_should_match": 1,
							"should": [
								{"multi_match": {"query": "pasta", "fields": ["title^3", "channel"]}},
								{"nested": {
									"path": "ingredients",
									"query": {"multi_match": {"query": "pasta", "fields": ["ingredients.name"]}}
								}}
							]
						}}
					],
					"filter": [
						{"nested": {
							"path": "ingredients",
							"query": {"term": {"ingredients.name.keyword": "egg"}}
						}}
					]
				}}
			}`,
		},
		{
			name:  "blank ingredients are dropped rather than filtering on nothing",
			query: Query{Text: "  cookies  ", Ingredients: []string{"", "   "}, Limit: 1},
			want: `{
				"size": 1,
				"_source": true,
				"query": {"bool": {"must": [
					{"bool": {
						"minimum_should_match": 1,
						"should": [
							{"multi_match": {"query": "cookies", "fields": ["title^3", "channel"]}},
							{"nested": {
								"path": "ingredients",
								"query": {"multi_match": {"query": "cookies", "fields": ["ingredients.name"]}}
							}}
						]
					}}
				]}}
			}`,
		},
		{
			name:  "an oversized limit is capped",
			query: Query{Limit: 10000},
			want: `{
				"size": 100,
				"_source": true,
				"query": {"match_all": {}},
				"sort": [{"indexed_at": {"order": "desc"}}]
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normaliseJSON(t, buildSearchBody(tc.query))
			want := parseJSON(t, tc.want)
			if !reflect.DeepEqual(got, want) {
				gotRaw, _ := json.MarshalIndent(got, "", "  ")
				wantRaw, _ := json.MarshalIndent(want, "", "  ")
				t.Errorf("query body mismatch\ngot:\n%s\nwant:\n%s", gotRaw, wantRaw)
			}
		})
	}
}

func TestQueryNormalise(t *testing.T) {
	cases := []struct {
		name            string
		in              Query
		wantText        string
		wantLimit       int
		wantIngredients []string
	}{
		{"defaults", Query{}, "", DefaultLimit, nil},
		{"trims text", Query{Text: "  pasta \n"}, "pasta", DefaultLimit, nil},
		{"negative limit falls back", Query{Limit: -5}, "", DefaultLimit, nil},
		{"limit is capped", Query{Limit: MaxLimit + 1}, "", MaxLimit, nil},
		{"limit is respected", Query{Limit: 7}, "", 7, nil},
		{"ingredients are lower-cased and trimmed",
			Query{Ingredients: []string{" Egg ", "MILK"}}, "", DefaultLimit, []string{"egg", "milk"}},
		{"blank ingredients are dropped",
			Query{Ingredients: []string{"", "  ", "egg"}}, "", DefaultLimit, []string{"egg"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalise()
			if got.Text != tc.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tc.wantText)
			}
			if got.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", got.Limit, tc.wantLimit)
			}
			if !reflect.DeepEqual(got.Ingredients, tc.wantIngredients) {
				t.Errorf("Ingredients = %v, want %v", got.Ingredients, tc.wantIngredients)
			}
		})
	}
}

// TestIndexMapping guards the three properties the query builder depends on:
// ingredients must be nested, the ingredient keyword must be case-normalised,
// and the title must have both an analysed and an exact form.
func TestIndexMapping(t *testing.T) {
	mapping := normaliseJSON(t, indexMapping())

	props := mapping["mappings"].(map[string]any)["properties"].(map[string]any)

	ingredients := props["ingredients"].(map[string]any)
	if ingredients["type"] != "nested" {
		t.Errorf("ingredients type = %v, want nested", ingredients["type"])
	}

	name := ingredients["properties"].(map[string]any)["name"].(map[string]any)
	keyword := name["fields"].(map[string]any)["keyword"].(map[string]any)
	if keyword["normalizer"] != "lowercase_normalizer" {
		t.Errorf("ingredient keyword normalizer = %v, want lowercase_normalizer", keyword["normalizer"])
	}

	title := props["title"].(map[string]any)
	if title["analyzer"] != "english" {
		t.Errorf("title analyzer = %v, want english", title["analyzer"])
	}
	if _, ok := title["fields"].(map[string]any)["keyword"]; !ok {
		t.Error("title has no keyword subfield")
	}

	for _, field := range []string{"video_id", "channel", "source", "model_version"} {
		if props[field].(map[string]any)["type"] != "keyword" {
			t.Errorf("%s is not a keyword field", field)
		}
	}
	if props["indexed_at"].(map[string]any)["type"] != "date" {
		t.Error("indexed_at is not a date field")
	}
}

// normaliseJSON round-trips a value through encoding/json so the comparison is
// against what Elasticsearch actually receives, not against Go's types.
func normaliseJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return out
}

func parseJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("test fixture is not valid JSON: %v", err)
	}
	return out
}
