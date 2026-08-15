package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"market-mate/config"
	"market-mate/handlers"
	"market-mate/middleware"
	"market-mate/models"
	"market-mate/services"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// healthCheck is the container healthcheck. The distroless runtime image has
// no shell and no curl, so the binary probes itself: `market-mate -health`
// exits 0 when the service is up and non-zero otherwise.
func healthCheck(port string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	health := flag.Bool("health", false, "probe the local health endpoint and exit (container healthcheck)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if *health {
		healthCheck(cfg.Port)
	}

	providers, simulated := buildProviders(cfg)

	videoHandler := handlers.NewVideoHandler(handlers.VideoHandlerConfig{
		VideoService:        providers.video,
		StoreFinder:         providers.stores,
		IngredientExtractor: providers.ingredients,
		CacheService:        services.NewCacheService(),
		LocationService:     services.NewLocationService(),
		Simulated:           simulated,
	})

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.NewRateLimiter().RateLimit())

	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.POST("/api/process-video", videoHandler.ProcessVideo)
	r.GET("/api/health", videoHandler.Health)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("MarketMate listening on :%s  (CORS: %v)", cfg.Port, cfg.AllowedOrigins)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	log.Println("stopped")
}

type providerSet struct {
	video       services.VideoProvider
	ingredients services.IngredientProvider
	stores      services.StoreProvider
}

// buildProviders picks a live or fixture implementation per capability.
//
// The service no longer refuses to start when a key is missing: it substitutes
// the fixture for that one provider and says so. A developer with only an
// OpenAI key gets live extraction against simulated video and store data, and
// someone who just cloned the repo gets a working demo of all three (FR-002).
func buildProviders(cfg *config.Config) (providerSet, models.Provenance) {
	var set providerSet
	var sim models.Provenance

	if cfg.HasYouTube() {
		if v, err := services.NewVideoService(cfg.YouTubeAPIKey); err == nil {
			set.video = v
		} else {
			log.Printf("YouTube client failed to initialise (%v); falling back to fixtures", err)
		}
	}
	if set.video == nil {
		set.video = services.NewFixtureVideoProvider()
		sim.Video = true
	}

	if cfg.HasOpenAI() {
		set.ingredients = services.NewIngredientExtractor(cfg.OpenAIAPIKey)
	} else {
		set.ingredients = services.NewFixtureIngredientProvider()
		sim.Ingredients = true
	}

	if cfg.HasMaps() {
		if s, err := services.NewStoreFinder(cfg.MapsAPIKey); err == nil {
			set.stores = s
		} else {
			log.Printf("Maps client failed to initialise (%v); falling back to fixtures", err)
		}
	}
	if set.stores == nil {
		set.stores = services.NewFixtureStoreProvider()
		sim.Stores = true
	}

	sim.Any = sim.Video || sim.Ingredients || sim.Stores

	log.Printf("providers — video: %s, ingredients: %s, stores: %s",
		mode(sim.Video), mode(sim.Ingredients), mode(sim.Stores))
	if sim.Any {
		log.Println("running in DEMO MODE: some results are simulated and labelled as such in the API response")
	}
	return set, sim
}

func mode(simulated bool) string {
	if simulated {
		return "simulated"
	}
	return "live"
}
