package config

import (
	"os"
	"strings"

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
		ForceDemo:     isTruthy(os.Getenv("DEMO_MODE")),
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
