package main

import (
	"log"
	"market-mate/config"
	"market-mate/handlers"
	"market-mate/middleware"
	"market-mate/services"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Initialize services
	videoService, err := services.NewVideoService(cfg.YouTubeAPIKey)
	if err != nil {
		log.Fatalf("Error creating video service: %v", err)
	}

	storeFinder, err := services.NewStoreFinder(cfg.MapsAPIKey)
	if err != nil {
		log.Fatalf("Error creating store finder: %v", err)
	}

	ingredientExtractor := services.NewIngredientExtractor(cfg.OpenAIAPIKey)
	cacheService := services.NewCacheService()
	locationService := services.NewLocationService()
	rateLimiter := middleware.NewRateLimiter()

	// Initialize handler with services
	videoHandler := handlers.NewVideoHandler(handlers.VideoHandlerConfig{
		VideoService:        videoService,
		StoreFinder:         storeFinder,
		IngredientExtractor: ingredientExtractor,
		CacheService:        cacheService,
		LocationService:     locationService,
	})

	//videoHandler := handlers.NewVideoHandler(videoService, storeFinder, ingredientExtractor, cacheService, locationService)

	// Setup Gin
	r := gin.Default()

	// Add middleware
	r.Use(middleware.Logger())
	r.Use(rateLimiter.RateLimit())

	// Configure CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"POST", "GET"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Routes
	r.POST("/api/process-video", videoHandler.ProcessVideo)

	log.Printf("Server starting on port %s", cfg.Port)
	log.Fatal(r.Run(":" + cfg.Port))
}
