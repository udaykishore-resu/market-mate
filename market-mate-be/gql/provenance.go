package gql

import (
	"fmt"
	"strings"

	"market-mate/health"
	"market-mate/models"

	"github.com/graphql-go/graphql"
)

// provenance is the GraphQL view of where an object's data came from.
//
// It is computed at the edge of the schema rather than carried on every model,
// but it is never invented: a recipe's flags come from the row's own source and
// model version, so a fixture extraction that has been through Postgres and
// Elasticsearch still reports itself as simulated.
type provenance struct {
	Source      string
	Simulated   bool
	Video       bool
	Ingredients bool
	Stores      bool
	Notice      string
}

// fixtureModelPrefix is how a fixture-produced extraction identifies itself.
// The extractor composes "<model>@<prompt fingerprint>", and the fixture
// provider uses the model name "fixture", so this prefix cannot collide with a
// live model.
var fixtureModelPrefix = models.SourceFixture + "@"

func recipeProvenance(r models.Recipe) provenance {
	video := r.Source == models.SourceFixture
	ingredients := r.Simulated || strings.HasPrefix(r.ModelVersion, fixtureModelPrefix)

	p := provenance{
		Source:      r.Source,
		Video:       video,
		Ingredients: ingredients,
		Simulated:   video || ingredients || r.Simulated,
	}
	if p.Simulated {
		p.Notice = "Simulated data: this recipe was produced by the fixture providers, not by YouTube and a live model."
	}
	return p
}

func storeProvenance(sim models.Provenance) provenance {
	p := provenance{
		Source:    models.SourceYouTube,
		Stores:    sim.Stores,
		Simulated: sim.Stores,
	}
	if sim.Stores {
		p.Source = models.SourceFixture
		p.Notice = "Simulated data: these shops are synthesised around the requested coordinate because no Maps key is configured."
	}
	return p
}

func resolveProvenanceField(get func(provenance) any) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		src, ok := p.Source.(provenance)
		if !ok {
			return nil, fmt.Errorf("provenance: unexpected source %T", p.Source)
		}
		return get(src), nil
	}
}

// newHealthType mirrors the REST health payload. It resolves through the same
// health.Checker, so the two transports cannot report different states.
func newHealthType() *graphql.Object {
	checkType := graphql.NewObject(graphql.ObjectConfig{
		Name: "DependencyCheck",
		Fields: graphql.Fields{
			"name": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: checkField},
			"ok":   &graphql.Field{Type: graphql.NewNonNull(graphql.Boolean), Resolve: checkField},
			"state": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: `"up", "down" or "disabled". A disabled dependency is a choice, not a fault.`,
				Resolve:     checkField,
			},
			"impl":      &graphql.Field{Type: graphql.String, Resolve: checkField},
			"latencyMs": &graphql.Field{Type: graphql.Int, Resolve: checkField},
			"error":     &graphql.Field{Type: graphql.String, Resolve: checkField},
		},
	})

	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Health",
		Fields: graphql.Fields{
			"status":  &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: healthField},
			"service": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: healthField},
			"mode": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.String),
				Description: `"live" or "demo".`,
				Resolve:     healthField,
			},
			"modelVersion": &graphql.Field{Type: graphql.String, Resolve: healthField},
			"checks": &graphql.Field{
				Type:    graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(checkType))),
				Resolve: healthField,
			},
		},
	})
}

func healthField(p graphql.ResolveParams) (any, error) {
	r, ok := p.Source.(health.Report)
	if !ok {
		return nil, fmt.Errorf("health: unexpected source %T", p.Source)
	}
	switch p.Info.FieldName {
	case "status":
		return r.Status, nil
	case "service":
		return r.Service, nil
	case "mode":
		return r.Mode, nil
	case "modelVersion":
		return r.ModelVersion, nil
	case "checks":
		return []health.Status{r.Checks.Postgres, r.Checks.Redis, r.Checks.Elasticsearch}, nil
	}
	return nil, fmt.Errorf("health: unknown field %s", p.Info.FieldName)
}

func checkField(p graphql.ResolveParams) (any, error) {
	s, ok := p.Source.(health.Status)
	if !ok {
		return nil, fmt.Errorf("check: unexpected source %T", p.Source)
	}
	switch p.Info.FieldName {
	case "name":
		return s.Name, nil
	case "ok":
		return s.OK, nil
	case "state":
		return s.State, nil
	case "impl":
		return s.Impl, nil
	case "latencyMs":
		return int(s.LatencyMS), nil
	case "error":
		return s.Error, nil
	}
	return nil, fmt.Errorf("check: unknown field %s", p.Info.FieldName)
}
