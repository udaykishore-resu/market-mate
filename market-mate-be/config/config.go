package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	YouTubeAPIKey string
	MapsAPIKey    string
	OpenAIAPIKey  string
	Port          string
}

func LoadConfig() (*Config, error) {
	godotenv.Load()

	config := &Config{
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
		MapsAPIKey:    os.Getenv("MAPS_API_KEY"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		Port:          os.Getenv("PORT"),
	}

	if config.Port == "" {
		config.Port = "8080"
	}

	return config, nil
}
