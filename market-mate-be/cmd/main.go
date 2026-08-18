package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"market-mate/config"
	"market-mate/gql"
	"market-mate/handlers"
	"market-mate/health"
	"market-mate/middleware"
	"market-mate/models"
	"market-mate/search"
	"market-mate/services"
	"market-mate/storage"

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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	healthFlag := flag.Bool("health", false, "probe the local health endpoint and exit (container healthcheck)")
	flag.Parse()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if *healthFlag {
		healthCheck(cfg.Port)
	}

	// Boot context: dialling dependencies must not hang the process. Each
	// dependency that fails to come up is logged and skipped, not fatal.
	bootCtx, bootCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer bootCancel()

	providers, simulated := buildProviders(cfg)
	deps := buildDependencies(bootCtx, cfg)
	defer deps.close()

	// One lookup shared by both transports: two would each hold their own
	// in-flight map and could issue the same Places call twice.
	storeLookup := services.NewStoreLookup(providers.stores, deps.cache, cfg.StoreCacheTTL)

	ready := &atomic.Bool{}
	ready.Store(true)

	videoHandler := handlers.NewVideoHandler(handlers.VideoHandlerConfig{
		VideoService:        providers.video,
		StoreFinder:         providers.stores,
		IngredientExtractor: providers.ingredients,
		CacheService:        deps.cache,
		LocationService:     services.NewLocationService(),
		StoreLookup:         storeLookup,
		Store:               deps.store,
		Search:              deps.search,
		StoreCacheTTL:       cfg.StoreCacheTTL,
		Simulated:           simulated,
		Ready:               ready,
	})

	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.NewRateLimiter().RateLimit())

	corsCfg := cors.Config{
		AllowOrigins:     cfg.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	if cfg.AllowLoopbackOrigins {
		// AllowOriginFunc supersedes AllowOrigins in gin-contrib/cors, and is
		// the only way to accept an arbitrary port while keeping
		// AllowCredentials — a literal "*" is rejected outright with
		// credentials enabled.
		corsCfg.AllowOrigins = nil
		corsCfg.AllowOriginFunc = config.IsLoopbackOrigin
	}
	r.Use(cors.New(corsCfg))

	r.POST("/api/process-video", videoHandler.ProcessVideo)
	r.GET("/api/health", videoHandler.Health)
	r.GET("/api/recipes/search", videoHandler.SearchRecipes)
	r.POST("/api/admin/reindex", videoHandler.Reindex)

	if cfg.GraphQL {
		mountGraphQL(r, cfg, deps, storeLookup, simulated, videoHandler.ModelVersion())
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// stopped is closed once shutdown has finished, so main does not return —
	// and does not run the deferred Close calls — while requests are draining.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("shutting down")
		// Fail the readiness probe first: a load balancer that keeps routing
		// here during the drain window turns a clean deploy into dropped
		// requests.
		ready.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	corsDescription := fmt.Sprintf("%v", cfg.AllowedOrigins)
	if cfg.AllowLoopbackOrigins {
		corsDescription = "any loopback origin (demo mode, ALLOWED_ORIGINS unset)"
	}
	log.Printf("MarketMate listening on :%s  (CORS: %s)", cfg.Port, corsDescription)
	log.Printf("extraction model version: %s", videoHandler.ModelVersion())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	<-stopped
	log.Println("stopped")
}

func mountGraphQL(r *gin.Engine, cfg *config.Config, deps *dependencies, stores *services.StoreLookup, simulated models.Provenance, modelVersion string) {
	schema, err := gql.New(gql.Config{
		Store:  deps.store,
		Search: deps.search,
		Stores: stores,
		Checker: health.Checker{
			Store:        deps.store,
			Cache:        deps.cache,
			Search:       deps.search,
			Simulated:    simulated,
			ModelVersion: modelVersion,
		},
		Simulated: simulated,
	})
	if err != nil {
		// A schema that does not build is a programming error, not a
		// configuration one, and it must not take the REST API down with it.
		log.Printf("GraphQL schema failed to build (%v); /graphql is not mounted", err)
		return
	}

	r.POST("/graphql", gin.WrapH(gql.NewHTTPHandler(&schema, false)))
	if cfg.GraphiQL {
		r.GET("/graphiql", gin.WrapH(gql.NewHTTPHandler(&schema, true)))
	}
	log.Printf("GraphQL mounted at POST /graphql (GraphiQL: %v)", cfg.GraphiQL)
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

// dependencies holds the optional infrastructure. Each field is what it
// resolved to, not what was asked for.
type dependencies struct {
	store  *storage.Store
	cache  services.Cache
	search search.Searcher
}

func (d *dependencies) close() {
	if d.store != nil {
		d.store.Close()
	}
	if d.cache != nil {
		if err := d.cache.Close(); err != nil {
			log.Printf("closing cache: %v", err)
		}
	}
}

// buildDependencies resolves Postgres, Redis and Elasticsearch, in the same
// per-capability style as the providers: anything unset or unreachable degrades
// to the behaviour the service had before that dependency existed.
//
// The single log line at the end is the point of this function. "which cache am
// I actually using" is the first question anyone asks of a deployment with
// optional infrastructure, and it should not require reading the environment
// back out of a container.
func buildDependencies(ctx context.Context, cfg *config.Config) *dependencies {
	deps := &dependencies{}

	if cfg.HasPostgres() {
		store, err := storage.Open(ctx, cfg.PostgresDSN)
		if err != nil {
			log.Printf("Postgres unavailable (%v); transcripts and extractions will not be cached across restarts", err)
		} else {
			deps.store = store
			if cfg.Migrate {
				if err := store.Migrate(ctx); err != nil {
					// A schema that did not apply cannot be read from safely.
					log.Printf("migrations failed (%v); disabling the Postgres cache", err)
					store.Close()
					deps.store = nil
				}
			}
		}
	}

	if cfg.HasRedis() {
		c, err := services.NewRedisCache(ctx, services.RedisOptions{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err != nil {
			log.Printf("Redis unavailable (%v); falling back to the in-memory cache", err)
		} else {
			deps.cache = c
		}
	}
	if deps.cache == nil {
		deps.cache = services.NewMemoryCache(cfg.StoreCacheTTL)
	}

	if cfg.HasElastic() {
		client, err := search.NewClient(cfg.ElasticURL, cfg.ElasticIndex)
		if err != nil {
			log.Printf("Elasticsearch misconfigured (%v); falling back", err)
		} else if err := client.EnsureIndex(ctx); err != nil {
			log.Printf("Elasticsearch unavailable (%v); falling back", err)
		} else {
			deps.search = client
		}
	}
	if deps.search == nil {
		if deps.store != nil {
			deps.search = search.NewDBSearcher(deps.store)
		} else {
			deps.search = search.Disabled{}
		}
	}

	postgres := "disabled"
	if deps.store != nil {
		postgres = "postgres"
	}
	log.Printf("dependencies — store: %s, cache: %s, search: %s",
		postgres, deps.cache.Kind(), deps.search.Kind())
	return deps
}

func mode(simulated bool) string {
	if simulated {
		return "simulated"
	}
	return "live"
}
