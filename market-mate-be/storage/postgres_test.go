package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"market-mate/models"
)

// The store is exercised against a real Postgres or not at all. A fake would
// test the fake: the things that can go wrong here are the advisory lock, the
// ON CONFLICT clauses, the JSONB round trip and the view's DISTINCT ON, and none
// of those exist outside Postgres.
//
// To run it:
//
//	docker run --rm -e POSTGRES_PASSWORD=marketmate -e POSTGRES_USER=marketmate \
//	  -e POSTGRES_DB=marketmate -p 5432:5432 postgres:17-alpine
//	MM_TEST_POSTGRES_DSN='postgres://marketmate:marketmate@localhost:5432/marketmate?sslmode=disable' \
//	  go test ./storage/ -v
//
// The dev-stack repo brings the same database up as part of `make up`.
func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("MM_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MM_TEST_POSTGRES_DSN is not set, so there is no database to test against. " +
			"Start one with `docker run --rm -e POSTGRES_PASSWORD=marketmate -e POSTGRES_USER=marketmate " +
			"-e POSTGRES_DB=marketmate -p 5432:5432 postgres:17-alpine` and set " +
			"MM_TEST_POSTGRES_DSN=postgres://marketmate:marketmate@localhost:5432/marketmate?sslmode=disable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to MM_TEST_POSTGRES_DSN: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		t.Fatalf("migrating: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// cleanup removes only this test's rows, so the suite can run against a
// database that has other data in it.
func cleanup(t *testing.T, s *Store, videoIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, id := range videoIDs {
			if _, err := s.pool.Exec(ctx, `DELETE FROM videos WHERE video_id = $1`, id); err != nil {
				t.Logf("cleanup %s: %v", id, err)
			}
		}
	})
}

func TestStoreVideoRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const videoID = "test_video_1"
	cleanup(t, store, videoID)

	if _, found, err := store.Video(ctx, videoID); err != nil || found {
		t.Fatalf("Video before insert: found=%v err=%v", found, err)
	}

	want := Video{
		VideoID:         videoID,
		Title:           "Perfect Spaghetti Carbonara",
		Channel:         "Trattoria Basics",
		DurationSeconds: 902,
		Transcript:      "400g spaghetti\n200g guanciale",
		Source:          models.SourceYouTube,
	}
	if err := store.SaveVideo(ctx, want); err != nil {
		t.Fatalf("SaveVideo: %v", err)
	}

	got, found, err := store.Video(ctx, videoID)
	if err != nil || !found {
		t.Fatalf("Video after insert: found=%v err=%v", found, err)
	}
	if got.Title != want.Title || got.Transcript != want.Transcript ||
		got.DurationSeconds != want.DurationSeconds || got.Source != want.Source {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
	if got.FetchedAt.IsZero() {
		t.Error("fetched_at was not set")
	}

	// A re-fetch refreshes rather than duplicating or erroring.
	want.Title = "Perfect Spaghetti Carbonara (updated)"
	if err := store.SaveVideo(ctx, want); err != nil {
		t.Fatalf("SaveVideo on conflict: %v", err)
	}
	got, _, _ = store.Video(ctx, videoID)
	if got.Title != want.Title {
		t.Errorf("title after upsert = %q, want %q", got.Title, want.Title)
	}
}

// TestExtractionsAreKeyedByModelVersion is the invalidation guarantee: two
// prompts coexist, and asking for one never returns the other's output.
func TestExtractionsAreKeyedByModelVersion(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const videoID = "test_video_2"
	cleanup(t, store, videoID)

	if err := store.SaveVideo(ctx, Video{VideoID: videoID, Source: models.SourceYouTube}); err != nil {
		t.Fatalf("SaveVideo: %v", err)
	}

	oldVersion := "gpt-4o-mini@aaaaaaaaaaaa"
	newVersion := "gpt-4o-mini@bbbbbbbbbbbb"

	if err := store.SaveExtraction(ctx, Extraction{
		VideoID:      videoID,
		ModelVersion: oldVersion,
		Ingredients:  []models.Ingredient{{Name: "Spaghetti", Quantity: "400 g"}},
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}

	if _, found, err := store.Extraction(ctx, videoID, newVersion); err != nil || found {
		t.Fatalf("a new prompt fingerprint must miss: found=%v err=%v", found, err)
	}

	got, found, err := store.Extraction(ctx, videoID, oldVersion)
	if err != nil || !found {
		t.Fatalf("Extraction: found=%v err=%v", found, err)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].Name != "Spaghetti" ||
		got.Ingredients[0].Quantity != "400 g" {
		t.Errorf("ingredients did not survive the JSONB round trip: %+v", got.Ingredients)
	}

	// Both versions coexist, so a rollback finds its own output rather than the
	// newer prompt's.
	if err := store.SaveExtraction(ctx, Extraction{
		VideoID:      videoID,
		ModelVersion: newVersion,
		Ingredients:  []models.Ingredient{{Name: "Spaghetti", Quantity: "400 g"}, {Name: "Guanciale", Quantity: "200 g"}},
	}); err != nil {
		t.Fatalf("SaveExtraction (second version): %v", err)
	}
	if old, _, _ := store.Extraction(ctx, videoID, oldVersion); len(old.Ingredients) != 1 {
		t.Errorf("the older extraction changed when a new version was written: %+v", old.Ingredients)
	}
}

// TestRecipesViewPrefersTheNewestExtraction covers the DISTINCT ON: the view
// must show one row per video, and it must be the current one.
func TestRecipesViewPrefersTheNewestExtraction(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const videoID = "test_video_3"
	cleanup(t, store, videoID)

	if err := store.SaveVideo(ctx, Video{
		VideoID: videoID,
		Title:   "Weeknight Thai Green Curry",
		Channel: "Bangkok Home Kitchen",
		Source:  models.SourceFixture,
	}); err != nil {
		t.Fatalf("SaveVideo: %v", err)
	}

	for _, e := range []Extraction{
		{VideoID: videoID, ModelVersion: "old@aaaaaaaaaaaa", Ingredients: []models.Ingredient{{Name: "old", Quantity: "1"}}},
		{VideoID: videoID, ModelVersion: "new@bbbbbbbbbbbb", Ingredients: []models.Ingredient{{Name: "Green curry paste", Quantity: "2 tbsp"}}},
	} {
		if err := store.SaveExtraction(ctx, e); err != nil {
			t.Fatalf("SaveExtraction: %v", err)
		}
		time.Sleep(5 * time.Millisecond) // distinct extracted_at values
	}

	recipe, found, err := store.Recipe(ctx, videoID)
	if err != nil || !found {
		t.Fatalf("Recipe: found=%v err=%v", found, err)
	}
	if recipe.ModelVersion != "new@bbbbbbbbbbbb" {
		t.Errorf("view returned model version %q, want the newest", recipe.ModelVersion)
	}
	if !recipe.Simulated {
		t.Error("a row written by the fixture provider came back unlabelled")
	}
	if recipe.Title != "Weeknight Thai Green Curry" {
		t.Errorf("title = %q", recipe.Title)
	}
}

func TestSearchRecipesFallback(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	const videoID = "test_video_4"
	cleanup(t, store, videoID)

	if err := store.SaveVideo(ctx, Video{
		VideoID: videoID, Title: "Creamy Tuscan Butter Salmon",
		Channel: "One Pan Wonders", Source: models.SourceYouTube,
	}); err != nil {
		t.Fatalf("SaveVideo: %v", err)
	}
	if err := store.SaveExtraction(ctx, Extraction{
		VideoID: videoID, ModelVersion: "gpt-4o-mini@cccccccccccc",
		Ingredients: []models.Ingredient{
			{Name: "Salmon fillets", Quantity: "4"},
			{Name: "Baby spinach", Quantity: "100 g"},
		},
	}); err != nil {
		t.Fatalf("SaveExtraction: %v", err)
	}

	cases := []struct {
		name        string
		query       string
		ingredients []string
		wantFound   bool
	}{
		{"title match", "tuscan", nil, true},
		{"case insensitive", "TUSCAN", nil, true},
		{"channel match", "One Pan", nil, true},
		{"ingredient text match", "spinach", nil, true},
		{"no match", "sauerkraut", nil, false},
		{"ingredient filter", "", []string{"baby spinach"}, true},
		{"ingredient filter is case insensitive", "", []string{"BABY SPINACH"}, true},
		{"ingredient filter excludes", "", []string{"guanciale"}, false},
		{"empty query lists everything", "", nil, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := store.SearchRecipes(ctx, tc.query, tc.ingredients, 50)
			if err != nil {
				t.Fatalf("SearchRecipes: %v", err)
			}
			var found bool
			for _, r := range got {
				if r.VideoID == videoID {
					found = true
				}
			}
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v (%d results)", found, tc.wantFound, len(got))
			}
		})
	}
}

// TestMigrateIsIdempotentAndConcurrencySafe reproduces the deploy that rolls
// several replicas at once: without the advisory lock, two of them race on the
// same empty schema_migrations and the loser crashes.
func TestMigrateIsIdempotentAndConcurrencySafe(t *testing.T) {
	store := testStore(t)

	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			errs <- store.Migrate(ctx)
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent migration %d: %v", i, err)
		}
	}

	ctx := context.Background()
	var applied int
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE name = $1`, "0001_init.sql").Scan(&applied); err != nil {
		t.Fatalf("counting applied migrations: %v", err)
	}
	if applied != 1 {
		t.Errorf("0001_init.sql recorded %d times, want exactly 1", applied)
	}
}

func TestPingReportsAWorkingConnection(t *testing.T) {
	store := testStore(t)
	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
