// Package gql exposes the pipeline's stored data over GraphQL.
//
// REST stays the pipeline's front door: POST /api/process-video is a command
// with three upstream calls and a 30 second ceiling. GraphQL is for reading
// what that produced — one round trip for a recipe, its ingredients and the
// shops that stock them, instead of the three the frontend makes today.
package gql

import (
	"fmt"
	"strings"

	"market-mate/health"
	"market-mate/models"
	"market-mate/search"
	"market-mate/services"
	"market-mate/storage"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"
)

// Config is what the schema resolves against. Every dependency is optional, in
// the same way it is for the REST handlers.
type Config struct {
	Store   *storage.Store
	Search  search.Searcher
	Stores  *services.StoreLookup
	Checker health.Checker

	// Simulated is the server's provenance, used for anything that is resolved
	// live rather than read back from a stored record.
	Simulated models.Provenance
}

// New builds the schema.
func New(cfg Config) (graphql.Schema, error) {
	if cfg.Search == nil {
		cfg.Search = search.Disabled{}
	}

	provenanceType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Provenance",
		Description: "Where this data came from. A fixture result must never be " +
			"presentable as a live one, so every type that can be simulated carries this.",
		Fields: graphql.Fields{
			"source": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: `"youtube" or "fixture".`,
				Resolve:     resolveProvenanceField(func(p provenance) any { return p.Source }),
			},
			"simulated": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.Boolean),
				Description: "True when any part of this object is fixture data.",
				Resolve:     resolveProvenanceField(func(p provenance) any { return p.Simulated }),
			},
			"video": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.Boolean),
				Resolve: resolveProvenanceField(func(p provenance) any { return p.Video }),
			},
			"ingredients": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.Boolean),
				Resolve: resolveProvenanceField(func(p provenance) any { return p.Ingredients }),
			},
			"stores": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.Boolean),
				Resolve: resolveProvenanceField(func(p provenance) any { return p.Stores }),
			},
			"notice": &graphql.Field{
				Type:        graphql.String,
				Description: "Human-readable explanation, present only when something is simulated.",
				Resolve:     resolveProvenanceField(func(p provenance) any { return p.Notice }),
			},
		},
	})

	storeType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Store",
		Fields: graphql.Fields{
			"name":     &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: storeField},
			"address":  &graphql.Field{Type: graphql.String, Resolve: storeField},
			"distance": &graphql.Field{Type: graphql.String, Resolve: storeField},
			"mapUrl":   &graphql.Field{Type: graphql.String, Resolve: storeField},
			"provenance": &graphql.Field{
				Type: graphql.NewNonNull(provenanceType),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					return storeProvenance(cfg.Simulated), nil
				},
			},
		},
	})

	// storeArgs are shared by every field that can resolve nearby shops. The
	// coordinate is an argument rather than server state because GraphQL has no
	// client IP to geolocate the way the REST pipeline does.
	storeArgs := graphql.FieldConfigArgument{
		"lat":          &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Float)},
		"lng":          &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Float)},
		"radiusMeters": &graphql.ArgumentConfig{Type: graphql.Int},
	}

	ingredientType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Ingredient",
		Fields: graphql.Fields{
			"name":     &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: ingredientField},
			"quantity": &graphql.Field{Type: graphql.String, Resolve: ingredientField},
			"stores": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(storeType))),
				Description: "Shops near the given point that would stock this ingredient. " +
					"Resolving it for every ingredient of a recipe costs one provider call, not one per ingredient.",
				Args:    storeArgs,
				Resolve: cfg.resolveStores(""),
			},
		},
	})

	recipeType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Recipe",
		Fields: graphql.Fields{
			"videoId":     &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: recipeField},
			"title":       &graphql.Field{Type: graphql.String, Resolve: recipeField},
			"channel":     &graphql.Field{Type: graphql.String, Resolve: recipeField},
			"ingredients": &graphql.Field{Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(ingredientType))), Resolve: recipeField},
			"modelVersion": &graphql.Field{
				Type:        graphql.String,
				Description: "The model and prompt fingerprint that produced this ingredient list.",
				Resolve:     recipeField,
			},
			"indexedAt": &graphql.Field{Type: graphql.DateTime, Resolve: recipeField},
			"provenance": &graphql.Field{
				Type: graphql.NewNonNull(provenanceType),
				Resolve: func(p graphql.ResolveParams) (any, error) {
					r, ok := p.Source.(models.Recipe)
					if !ok {
						return nil, fmt.Errorf("provenance: unexpected source %T", p.Source)
					}
					return recipeProvenance(r), nil
				},
			},
			"stores": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(storeType))),
				Args:    storeArgs,
				Resolve: cfg.resolveRecipeStores(),
			},
		},
	})

	healthType := newHealthType()

	query := graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"recipe": &graphql.Field{
				Type:        recipeType,
				Description: "One stored recipe, or null when that video has not been processed yet.",
				Args: graphql.FieldConfigArgument{
					"videoId": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
				},
				Resolve: cfg.resolveRecipe,
			},
			"searchRecipes": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(recipeType))),
				Args: graphql.FieldConfigArgument{
					"query":      &graphql.ArgumentConfig{Type: graphql.String},
					"ingredient": &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
					"limit":      &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: cfg.resolveSearch,
			},
			"stores": &graphql.Field{
				Type: graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(storeType))),
				Args: graphql.FieldConfigArgument{
					"videoId":      &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"lat":          &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Float)},
					"lng":          &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Float)},
					"radiusMeters": &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: cfg.resolveTopLevelStores,
			},
			"health": &graphql.Field{
				Type:    graphql.NewNonNull(healthType),
				Resolve: cfg.resolveHealth,
			},
		},
	})

	return graphql.NewSchema(graphql.SchemaConfig{Query: query})
}

// --- resolvers ----------------------------------------------------------------

func (cfg Config) resolveRecipe(p graphql.ResolveParams) (any, error) {
	videoID, _ := p.Args["videoId"].(string)
	videoID = strings.TrimSpace(videoID)
	if videoID == "" {
		return nil, fmt.Errorf("videoId must not be empty")
	}

	if cfg.Store != nil {
		r, found, err := cfg.Store.Recipe(p.Context, videoID)
		if err != nil {
			return nil, err
		}
		if found {
			return *r, nil
		}
		return nil, nil
	}

	// No Postgres: ask the search backend, which may still hold the document.
	// Matching on the returned ID rather than trusting relevance — a text search
	// for an eleven-character ID can hit anything.
	results, err := cfg.Search.Search(p.Context, search.Query{Text: videoID, Limit: search.MaxLimit})
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if r.VideoID == videoID {
			return r, nil
		}
	}
	return nil, nil
}

func (cfg Config) resolveSearch(p graphql.ResolveParams) (any, error) {
	q := search.Query{}
	if text, ok := p.Args["query"].(string); ok {
		q.Text = text
	}
	if limit, ok := p.Args["limit"].(int); ok {
		q.Limit = limit
	}
	if raw, ok := p.Args["ingredient"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				q.Ingredients = append(q.Ingredients, s)
			}
		}
	}

	results, err := cfg.Search.Search(p.Context, q)
	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []models.Recipe{}
	}
	return results, nil
}

func (cfg Config) resolveTopLevelStores(p graphql.ResolveParams) (any, error) {
	videoID, _ := p.Args["videoId"].(string)
	return cfg.stores(p, strings.TrimSpace(videoID))
}

func (cfg Config) resolveRecipeStores() graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		r, ok := p.Source.(models.Recipe)
		if !ok {
			return nil, fmt.Errorf("stores: unexpected source %T", p.Source)
		}
		return cfg.stores(p, r.VideoID)
	}
}

// resolveStores returns a resolver bound to a fixed cache identity.
//
// Ingredients pass the empty video ID on purpose: which shops are near a point
// does not depend on which recipe asked, so every ingredient in a query shares
// one cache entry. That is what turns N ingredients into one provider call
// instead of N.
func (cfg Config) resolveStores(videoID string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		return cfg.stores(p, videoID)
	}
}

func (cfg Config) stores(p graphql.ResolveParams, videoID string) (any, error) {
	if cfg.Stores == nil {
		return []models.Store{}, nil
	}
	lat, _ := p.Args["lat"].(float64)
	lng, _ := p.Args["lng"].(float64)
	radius, _ := p.Args["radiusMeters"].(int)

	found, _, err := cfg.Stores.Stores(p.Context, videoID, lat, lng)
	if err != nil {
		return nil, err
	}
	return services.WithinRadius(found, radius), nil
}

func (cfg Config) resolveHealth(p graphql.ResolveParams) (any, error) {
	return cfg.Checker.Report(p.Context), nil
}

// --- field accessors ----------------------------------------------------------
//
// Written out rather than left to the default struct resolver: the default
// matches on json tags, so renaming a tag for the REST response would silently
// blank a GraphQL field.

func recipeField(p graphql.ResolveParams) (any, error) {
	r, ok := p.Source.(models.Recipe)
	if !ok {
		return nil, fmt.Errorf("recipe: unexpected source %T", p.Source)
	}
	switch p.Info.FieldName {
	case "videoId":
		return r.VideoID, nil
	case "title":
		return r.Title, nil
	case "channel":
		return r.Channel, nil
	case "ingredients":
		return r.Ingredients, nil
	case "modelVersion":
		return r.ModelVersion, nil
	case "indexedAt":
		return r.IndexedAt, nil
	}
	return nil, fmt.Errorf("recipe: unknown field %s", p.Info.FieldName)
}

func ingredientField(p graphql.ResolveParams) (any, error) {
	i, ok := p.Source.(models.Ingredient)
	if !ok {
		return nil, fmt.Errorf("ingredient: unexpected source %T", p.Source)
	}
	switch p.Info.FieldName {
	case "name":
		return i.Name, nil
	case "quantity":
		return i.Quantity, nil
	}
	return nil, fmt.Errorf("ingredient: unknown field %s", p.Info.FieldName)
}

func storeField(p graphql.ResolveParams) (any, error) {
	s, ok := p.Source.(models.Store)
	if !ok {
		return nil, fmt.Errorf("store: unexpected source %T", p.Source)
	}
	switch p.Info.FieldName {
	case "name":
		return s.Name, nil
	case "address":
		return s.Address, nil
	case "distance":
		return s.Distance, nil
	case "mapUrl":
		return s.MapURL, nil
	}
	return nil, fmt.Errorf("store: unknown field %s", p.Info.FieldName)
}

// NewHTTPHandler mounts the schema. GraphiQL is opt-in via MM_GRAPHIQL because
// it is an unauthenticated schema browser; useful in development, not something
// to leave on a public deployment.
func NewHTTPHandler(schema *graphql.Schema, graphiql bool) *handler.Handler {
	return handler.New(&handler.Config{
		Schema:   schema,
		Pretty:   true,
		GraphiQL: graphiql,
	})
}
