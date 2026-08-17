package search

// indexMapping is the index definition.
//
// title gets the english analyser so "cookies" finds "cookie", plus a keyword
// subfield for exact-title lookups and sorting. Ingredients are nested rather
// than an object array because a flat array loses the association between a
// name and its quantity — "400 g" and "spaghetti" would match across different
// ingredients of the same recipe.
//
// The lowercase normaliser on the keyword subfield is what makes the
// `ingredient=` filter usable: a raw keyword field is case-sensitive, so
// ?ingredient=spaghetti would silently miss every recipe listing "Spaghetti".
func indexMapping() map[string]any {
	return map[string]any{
		"settings": map[string]any{
			"number_of_shards":   1,
			"number_of_replicas": 0,
			"analysis": map[string]any{
				"normalizer": map[string]any{
					"lowercase_normalizer": map[string]any{
						"type":   "custom",
						"filter": []string{"lowercase"},
					},
				},
			},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"video_id": map[string]any{"type": "keyword"},
				"title": map[string]any{
					"type":     "text",
					"analyzer": "english",
					"fields": map[string]any{
						"keyword": map[string]any{"type": "keyword", "ignore_above": 256},
					},
				},
				"channel": map[string]any{"type": "keyword"},
				"ingredients": map[string]any{
					"type": "nested",
					"properties": map[string]any{
						"name": map[string]any{
							"type":     "text",
							"analyzer": "english",
							"fields": map[string]any{
								"keyword": map[string]any{
									"type":       "keyword",
									"normalizer": "lowercase_normalizer",
								},
							},
						},
						"quantity": map[string]any{"type": "keyword"},
						"unit":     map[string]any{"type": "keyword"},
					},
				},
				"source":        map[string]any{"type": "keyword"},
				"simulated":     map[string]any{"type": "boolean"},
				"model_version": map[string]any{"type": "keyword"},
				"indexed_at":    map[string]any{"type": "date"},
			},
		},
	}
}

// buildSearchBody turns a Query into an Elasticsearch request body.
//
// Split out from the client so the query shape can be asserted in a test
// without a running cluster — a wrong field name here fails as "no results",
// which is indistinguishable from an empty index at the API boundary.
func buildSearchBody(q Query) map[string]any {
	q = q.Normalise()

	body := map[string]any{
		"size":    q.Limit,
		"_source": true,
	}

	var must []any
	if q.Text != "" {
		// Title and ingredient names live at different nesting levels, so one
		// multi_match cannot span them; the should/nested pair is the standard
		// way to score a document on either. Title is boosted: a recipe called
		// "Chocolate Chip Cookies" is a better hit for "cookies" than one that
		// merely lists chocolate chips.
		must = append(must, map[string]any{
			"bool": map[string]any{
				"minimum_should_match": 1,
				"should": []any{
					map[string]any{
						"multi_match": map[string]any{
							"query":  q.Text,
							"fields": []string{"title^3", "channel"},
						},
					},
					map[string]any{
						"nested": map[string]any{
							"path": "ingredients",
							"query": map[string]any{
								"multi_match": map[string]any{
									"query":  q.Text,
									"fields": []string{"ingredients.name"},
								},
							},
						},
					},
				},
			},
		})
	}

	var filter []any
	if len(q.Ingredients) == 1 {
		filter = append(filter, nestedIngredientFilter(map[string]any{
			"term": map[string]any{"ingredients.name.keyword": q.Ingredients[0]},
		}))
	} else if len(q.Ingredients) > 1 {
		// Repeated ?ingredient= is a terms filter: "any of these", which is what
		// a user ticking boxes in a filter list means.
		filter = append(filter, nestedIngredientFilter(map[string]any{
			"terms": map[string]any{"ingredients.name.keyword": q.Ingredients},
		}))
	}

	if len(must) == 0 && len(filter) == 0 {
		body["query"] = map[string]any{"match_all": map[string]any{}}
		// Nothing to score against, so order by recency rather than by a
		// constant relevance every document shares.
		body["sort"] = []any{map[string]any{"indexed_at": map[string]any{"order": "desc"}}}
		return body
	}

	boolQuery := map[string]any{}
	if len(must) > 0 {
		boolQuery["must"] = must
	}
	if len(filter) > 0 {
		boolQuery["filter"] = filter
	}
	body["query"] = map[string]any{"bool": boolQuery}
	return body
}

func nestedIngredientFilter(inner map[string]any) map[string]any {
	return map[string]any{
		"nested": map[string]any{
			"path":  "ingredients",
			"query": inner,
		},
	}
}
