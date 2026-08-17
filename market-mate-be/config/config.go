package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds runtime settings. Every field has a working default, so the
// service starts with an empty environment (FR-001) — the previous build
// required three API keys before it would come up at all, which meant nobody
// could run it, which is why its bugs went unnoticed.
type Config struct {
	YouTubeAPIKey string
	MapsAPIKey    string
	OpenAIAPIKey  string
	Port          string

	// AllowedOrigins is the CORS allow-list. Previously hardcoded to
	// http://localhost:5173, which meant any real deployment was broken on
	// arrival.
	AllowedOrigins []string

	// ForceDemo runs every provider from fixtures even when keys are present,
	// for screenshots and offline demos.
	ForceDemo bool

	// The MM_* block below is the platform contract's. Every one of these is
	// optional and every one of them degrades to the behaviour this service had
	// before it existed — see .env.example for the fallback each unset value
	// selects.
	PostgresDSN   string
	Migrate       bool
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	ElasticURL    string
	ElasticIndex  string
	StoreCacheTTL time.Duration
	GraphQL       bool
	GraphiQL      bool
}

func LoadConfig() (*Config, error) {
	// A missing .env is normal, not an error: in production the values come
	// from the environment.
	_ = godotenv.Load()

	cfg := &Config{
		YouTubeAPIKey: strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY")),
		MapsAPIKey:    strings.TrimSpace(os.Getenv("MAPS_API_KEY")),
		OpenAIAPIKey:  strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		Port:          envOr("PORT", "8080"),
		// USE_FIXTURES is the platform contract's name for this switch and
		// DEMO_MODE is the one this repo shipped with. Both are honoured: a
		// contract that renames a flag should not silently turn it off.
		ForceDemo: isTruthy(os.Getenv("DEMO_MODE")) || isTruthy(os.Getenv("USE_FIXTURES")),

		PostgresDSN:   strings.TrimSpace(os.Getenv("MM_POSTGRES_DSN")),
		Migrate:       isTruthy(envOr("MM_MIGRATE", "true")),
		RedisAddr:     strings.TrimSpace(os.Getenv("MM_REDIS_ADDR")),
		RedisPassword: os.Getenv("MM_REDIS_PASSWORD"),
		RedisDB:       intEnvOr("MM_REDIS_DB", 1),
		ElasticURL:    strings.TrimSpace(os.Getenv("MM_ELASTIC_URL")),
		ElasticIndex:  envOr("MM_ELASTIC_INDEX", "marketmate-recipes"),
		StoreCacheTTL: durationEnvOr("MM_STORE_CACHE_TTL", 15*time.Minute),
		GraphQL:       isTruthy(envOr("MM_GRAPHQL", "true")),
		// GraphiQL defaults off where GraphQL defaults on: the endpoint is
		// additive, but an unauthenticated schema browser is a decision.
		GraphiQL: isTruthy(os.Getenv("MM_GRAPHIQL")),
	}

	origins := envOr("ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:4173")
	for _, o := range strings.Split(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
		}
	}

	return cfg, nil
}

// HasYouTube etc. report whether a live provider can be built. Selection is
// per provider rather than all-or-nothing, so a developer with only an OpenAI
// key still gets live extraction alongside simulated video and stores.
func (c *Config) HasYouTube() bool { return !c.ForceDemo && c.YouTubeAPIKey != "" }
func (c *Config) HasOpenAI() bool  { return !c.ForceDemo && c.OpenAIAPIKey != "" }
func (c *Config) HasMaps() bool    { return !c.ForceDemo && c.MapsAPIKey != "" }

// HasPostgres etc. report which optional dependency is configured. Same
// per-capability selection as the provider keys: a developer with Redis but no
// Postgres gets the shared cache and the old re-fetch behaviour, not an error.
func (c *Config) HasPostgres() bool { return c.PostgresDSN != "" }
func (c *Config) HasRedis() bool    { return c.RedisAddr != "" }
func (c *Config) HasElastic() bool  { return c.ElasticURL != "" }

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// intEnvOr and durationEnvOr fall back rather than fail. A malformed value gets
// a warning and the default: refusing to boot over a mistyped TTL would take
// the service down for a setting it can carry on without.
func intEnvOr(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("config: %s=%q is not a number; using %d", key, raw, fallback)
		return fallback
	}
	return v
}

func durationEnvOr(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		log.Printf("config: %s=%q is not a positive duration (e.g. 15m); using %s", key, raw, fallback)
		return fallback
	}
	return v
}
