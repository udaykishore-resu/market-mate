// Package health builds the dependency report served by GET /api/health and by
// the GraphQL health field.
//
// It lives outside handlers because two callers need the same answer, and a
// second implementation of "is Redis up" is a second thing to be wrong.
package health

import (
	"context"
	"time"

	"market-mate/models"
	"market-mate/search"
	"market-mate/services"
	"market-mate/storage"
)

// checkTimeout is per dependency. The endpoint backs container healthchecks and
// Kubernetes probes, so the whole report has to return faster than the shortest
// probe timeout even when every dependency is hanging.
const checkTimeout = 2 * time.Second

const (
	StateUp = "up"
	// StateDown is a configured dependency that did not answer. It degrades the
	// service; it does not stop it.
	StateDown = "down"
	// StateDisabled is a dependency that was never configured. Not a failure:
	// every dependency here is opt-in, and an unset variable is a choice.
	StateDisabled = "disabled"
)

// Status is one dependency's line in the report.
//
// Impl names the implementation that actually resolved — "redis" or "memory",
// "elasticsearch" or "postgres". That is the field that answers the question an
// operator really has, which is not "is Redis up" but "is this deployment using
// the Redis I configured".
type Status struct {
	Name      string `json:"-"`
	OK        bool   `json:"ok"`
	State     string `json:"state"`
	Impl      string `json:"impl,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// Checks is the per-dependency block.
type Checks struct {
	Postgres      Status `json:"postgres"`
	Redis         Status `json:"redis"`
	Elasticsearch Status `json:"elasticsearch"`
	// Providers reports which implementation each capability resolved to, in
	// the vocabulary of the checks around it: "fixture" is the implementation,
	// where "simulated" describes the data it returns.
	Providers map[string]string `json:"providers"`
}

// Report is the whole health response.
type Report struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Mode    string `json:"mode"`
	// Providers is the flat block that predates Checks and is what existing
	// clients read; it stays.
	Providers    map[string]string   `json:"providers"`
	Checks       Checks              `json:"checks"`
	Cache        services.CacheStats `json:"cache"`
	ModelVersion string              `json:"model_version"`
	Time         time.Time           `json:"time"`
}

// Degraded reports whether any configured dependency failed its probe.
func (r Report) Degraded() bool { return r.Status == "degraded" }

// Checker holds the dependencies to probe. Every one of them may be absent.
type Checker struct {
	Store        *storage.Store
	Cache        services.Cache
	Search       search.Searcher
	Simulated    models.Provenance
	ModelVersion string
}

// Report probes every dependency and composes the response.
//
// Status is "degraded", not "down", when a configured dependency is
// unreachable: MarketMate still answers from its providers with no Postgres, no
// Redis and no Elasticsearch. Whether that warrants a non-200 is the caller's
// decision, because only the caller knows if it is serving or draining.
func (c Checker) Report(ctx context.Context) Report {
	checks := Checks{
		Postgres:      c.postgres(ctx),
		Redis:         c.redis(ctx),
		Elasticsearch: c.elasticsearch(ctx),
		Providers: map[string]string{
			"transcript":  implMode(c.Simulated.Video),
			"ingredients": implMode(c.Simulated.Ingredients),
			"stores":      implMode(c.Simulated.Stores),
		},
	}

	status := "ok"
	for _, dep := range []Status{checks.Postgres, checks.Redis, checks.Elasticsearch} {
		if !dep.OK {
			status = "degraded"
		}
	}

	mode := "live"
	if c.Simulated.Any {
		mode = "demo"
	}

	var stats services.CacheStats
	if c.Cache != nil {
		stats = c.Cache.Stats()
	}

	return Report{
		Status:  status,
		Service: "market-mate",
		Mode:    mode,
		Providers: map[string]string{
			"video":       providerMode(c.Simulated.Video),
			"ingredients": providerMode(c.Simulated.Ingredients),
			"stores":      providerMode(c.Simulated.Stores),
		},
		Checks:       checks,
		Cache:        stats,
		ModelVersion: c.ModelVersion,
		Time:         time.Now().UTC(),
	}
}

func (c Checker) postgres(ctx context.Context) Status {
	if c.Store == nil {
		return Status{Name: "postgres", OK: true, State: StateDisabled, Impl: "none"}
	}
	return probe(ctx, "postgres", "postgres", c.Store.Ping)
}

func (c Checker) redis(ctx context.Context) Status {
	if c.Cache == nil {
		return Status{Name: "redis", OK: true, State: StateDisabled, Impl: "none"}
	}
	kind := c.Cache.Kind()
	if kind != services.KindRedis {
		return Status{Name: "redis", OK: true, State: StateDisabled, Impl: kind}
	}
	return probe(ctx, "redis", kind, c.Cache.Ping)
}

func (c Checker) elasticsearch(ctx context.Context) Status {
	if c.Search == nil {
		return Status{Name: "elasticsearch", OK: true, State: StateDisabled, Impl: "none"}
	}
	kind := c.Search.Kind()
	if kind != search.KindElasticsearch {
		return Status{Name: "elasticsearch", OK: true, State: StateDisabled, Impl: kind}
	}
	return probe(ctx, "elasticsearch", kind, c.Search.Health)
}

func probe(ctx context.Context, name, impl string, fn func(context.Context) error) Status {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	start := time.Now()
	err := fn(ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Status{Name: name, State: StateDown, Impl: impl, LatencyMS: latency, Error: err.Error()}
	}
	return Status{Name: name, OK: true, State: StateUp, Impl: impl, LatencyMS: latency}
}

func implMode(simulated bool) string {
	if simulated {
		return models.SourceFixture
	}
	return "live"
}

func providerMode(simulated bool) string {
	if simulated {
		return "simulated"
	}
	return "live"
}
