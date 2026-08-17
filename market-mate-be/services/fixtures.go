package services

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"

	"market-mate/models"
)

// Fixture providers let the whole pipeline run with no API keys, no billing
// account, and no network. They are what makes `git clone && make demo` reach a
// working browser demo, and they are what the handler tests run against.
//
// They are not stubs that return one canned blob: the video ID selects a recipe
// and the coordinates shift the store names and distances, so clicking around
// the demo behaves like clicking around the product.

// --- Video -------------------------------------------------------------------

// FixtureVideoProvider serves recipes from a small built-in catalogue.
type FixtureVideoProvider struct{}

func NewFixtureVideoProvider() *FixtureVideoProvider { return &FixtureVideoProvider{} }

type demoRecipe struct {
	title       string
	channel     string
	description string
	// durationSeconds is plausible rather than arbitrary: it is stored on the
	// video row and shown as real metadata, so a zero here would read as a
	// broken record instead of a demo one.
	durationSeconds int
	ingredients     []models.Ingredient
}

var demoRecipes = []demoRecipe{
	{
		title:   "Perfect Spaghetti Carbonara in 15 Minutes",
		channel: "Trattoria Basics",
		description: `The real Roman carbonara — no cream, ever.

INGREDIENTS
400g spaghetti
200g guanciale, diced
4 large egg yolks
1 whole egg
100g Pecorino Romano, finely grated
2 tsp black peppercorns, coarsely cracked
Salt for the pasta water

Render the guanciale slowly. Whisk yolks with pecorino and pepper. Toss off the
heat with a splash of pasta water and keep it moving so it emulsifies.`,
		durationSeconds: 902,
		ingredients: []models.Ingredient{
			{Name: "Spaghetti", Quantity: "400 g"},
			{Name: "Guanciale, diced", Quantity: "200 g"},
			{Name: "Egg yolks", Quantity: "4 large"},
			{Name: "Whole egg", Quantity: "1"},
			{Name: "Pecorino Romano, finely grated", Quantity: "100 g"},
			{Name: "Black peppercorns, cracked", Quantity: "2 tsp"},
			{Name: "Salt, for pasta water", Quantity: "to taste"},
		},
	},
	{
		title:   "Weeknight Thai Green Curry",
		channel: "Bangkok Home Kitchen",
		description: `Thirty minutes, one pan.

What you need:
2 tbsp green curry paste
400ml coconut milk
500g chicken thigh, sliced
1 Thai eggplant, quartered
Handful of Thai basil
2 tbsp fish sauce
1 tbsp palm sugar
2 kaffir lime leaves
Jasmine rice to serve`,
		durationSeconds: 741,
		ingredients: []models.Ingredient{
			{Name: "Green curry paste", Quantity: "2 tbsp"},
			{Name: "Coconut milk", Quantity: "400 ml"},
			{Name: "Chicken thigh, sliced", Quantity: "500 g"},
			{Name: "Thai eggplant", Quantity: "1, quartered"},
			{Name: "Thai basil", Quantity: "1 handful"},
			{Name: "Fish sauce", Quantity: "2 tbsp"},
			{Name: "Palm sugar", Quantity: "1 tbsp"},
			{Name: "Kaffir lime leaves", Quantity: "2"},
			{Name: "Jasmine rice", Quantity: "to serve"},
		},
	},
	{
		title:   "The Only Chocolate Chip Cookie Recipe You Need",
		channel: "Flour & Salt",
		description: `Brown the butter. Rest the dough overnight. Trust me.

225g unsalted butter
200g dark brown sugar
100g caster sugar
2 eggs plus 1 yolk
1 tbsp vanilla extract
340g plain flour
1 tsp baking soda
1.5 tsp flaky sea salt
300g dark chocolate, chopped into shards`,
		durationSeconds: 1123,
		ingredients: []models.Ingredient{
			{Name: "Unsalted butter", Quantity: "225 g"},
			{Name: "Dark brown sugar", Quantity: "200 g"},
			{Name: "Caster sugar", Quantity: "100 g"},
			{Name: "Eggs", Quantity: "2, plus 1 yolk"},
			{Name: "Vanilla extract", Quantity: "1 tbsp"},
			{Name: "Plain flour", Quantity: "340 g"},
			{Name: "Baking soda", Quantity: "1 tsp"},
			{Name: "Flaky sea salt", Quantity: "1.5 tsp"},
			{Name: "Dark chocolate, chopped", Quantity: "300 g"},
		},
	},
	{
		title:   "Smash Burgers at Home (Better Than Takeout)",
		channel: "Cast Iron Club",
		description: `Screaming hot pan, thin patties, don't move them.

500g ground beef, 20% fat
4 potato buns
8 slices American cheese
1 white onion, sliced paper thin
Dill pickles
For the sauce: 4 tbsp mayonnaise, 2 tbsp ketchup, 1 tbsp yellow mustard,
1 tsp pickle brine
Neutral oil, salt, pepper`,
		durationSeconds: 688,
		ingredients: []models.Ingredient{
			{Name: "Ground beef (20% fat)", Quantity: "500 g"},
			{Name: "Potato buns", Quantity: "4"},
			{Name: "American cheese", Quantity: "8 slices"},
			{Name: "White onion, thinly sliced", Quantity: "1"},
			{Name: "Dill pickles", Quantity: "to taste"},
			{Name: "Mayonnaise", Quantity: "4 tbsp"},
			{Name: "Ketchup", Quantity: "2 tbsp"},
			{Name: "Yellow mustard", Quantity: "1 tbsp"},
			{Name: "Pickle brine", Quantity: "1 tsp"},
			{Name: "Neutral oil, salt, pepper", Quantity: "to taste"},
		},
	},
	{
		title:   "Creamy Tuscan Butter Salmon",
		channel: "One Pan Wonders",
		description: `Restaurant dinner, twenty minutes, one skillet.

4 salmon fillets, skin on
3 tbsp butter
4 garlic cloves, minced
200g cherry tomatoes, halved
100g baby spinach
250ml heavy cream
50g parmesan, grated
1 tsp Italian seasoning
Juice of half a lemon`,
		durationSeconds: 615,
		ingredients: []models.Ingredient{
			{Name: "Salmon fillets, skin on", Quantity: "4"},
			{Name: "Butter", Quantity: "3 tbsp"},
			{Name: "Garlic cloves, minced", Quantity: "4"},
			{Name: "Cherry tomatoes, halved", Quantity: "200 g"},
			{Name: "Baby spinach", Quantity: "100 g"},
			{Name: "Heavy cream", Quantity: "250 ml"},
			{Name: "Parmesan, grated", Quantity: "50 g"},
			{Name: "Italian seasoning", Quantity: "1 tsp"},
			{Name: "Lemon", Quantity: "1/2, juiced"},
		},
	},
}

// pick maps an arbitrary string onto a stable index, so the same video ID always
// yields the same recipe across restarts — a demo that shuffles under you looks
// broken rather than dynamic.
func pick(key string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(n))
}

func (f *FixtureVideoProvider) GetVideoDetails(_ context.Context, videoID string) (*VideoDetails, error) {
	r := demoRecipes[pick(videoID, len(demoRecipes))]
	return &VideoDetails{
		ID:           videoID,
		Title:        r.title,
		Description:  r.description,
		ChannelTitle: r.channel,
		// Real thumbnail URLs: YouTube serves these unauthenticated, so a demo
		// with a genuine video ID shows the genuine thumbnail.
		ThumbnailURL:    fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID),
		DurationSeconds: r.durationSeconds,
	}, nil
}

// --- Ingredients -------------------------------------------------------------

// FixtureIngredientProvider returns the curated list for whichever demo recipe
// the description belongs to, falling back to parsing the text directly.
type FixtureIngredientProvider struct{}

func NewFixtureIngredientProvider() *FixtureIngredientProvider { return &FixtureIngredientProvider{} }

// ModelVersion implements Versioned. The catalogue plays the part the prompt
// plays for the live extractor, so editing a demo recipe invalidates the rows
// this provider wrote instead of leaving the old list in Postgres forever.
//
// The "fixture" model name also keeps the two worlds apart: a live extractor's
// version string can never collide with this one, so a demo row is never read
// back on a request being answered live.
func (f *FixtureIngredientProvider) ModelVersion() string {
	return ModelVersion(models.SourceFixture, fixtureCatalogue())
}

func fixtureCatalogue() string {
	var b strings.Builder
	for _, r := range demoRecipes {
		b.WriteString(r.title)
		b.WriteByte('\n')
		for _, i := range r.ingredients {
			b.WriteString(i.Quantity)
			b.WriteString(" | ")
			b.WriteString(i.Name)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (f *FixtureIngredientProvider) ExtractIngredients(_ context.Context, description string) ([]models.Ingredient, error) {
	for _, r := range demoRecipes {
		if r.description == description {
			out := make([]models.Ingredient, len(r.ingredients))
			copy(out, r.ingredients)
			return out, nil
		}
	}
	// Not one of ours — parse the text so the fixture still does something
	// sensible with an arbitrary description.
	return parseIngredientLines(description), nil
}

// parseIngredientLines is a best-effort text parser, also used as the fallback
// when the live model returns prose instead of a clean list.
func parseIngredientLines(text string) []models.Ingredient {
	var out []models.Ingredient
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimLeft(line, "-*•· \t")
		if line == "" || len(line) > 120 {
			continue
		}
		// Skip prose: an ingredient line is short and rarely ends in a period.
		if strings.HasSuffix(line, ".") || len(strings.Fields(line)) > 10 {
			continue
		}
		if !strings.ContainsAny(line, "0123456789") &&
			!strings.Contains(strings.ToLower(line), "to taste") {
			continue
		}
		qty, name := splitQuantity(line)
		if name == "" {
			continue
		}
		out = append(out, models.Ingredient{Name: name, Quantity: qty})
	}
	return out
}

// splitQuantity separates a leading measurement from the ingredient name:
// "400g spaghetti" -> ("400g", "spaghetti").
func splitQuantity(line string) (quantity, name string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	units := map[string]bool{
		"g": true, "kg": true, "ml": true, "l": true, "tsp": true, "tbsp": true,
		"cup": true, "cups": true, "oz": true, "lb": true, "lbs": true,
		"clove": true, "cloves": true, "slice": true, "slices": true,
		"large": true, "small": true, "medium": true, "handful": true,
	}
	i := 0
	for i < len(fields) && i < 3 {
		f := strings.ToLower(strings.Trim(fields[i], ",."))
		hasDigit := strings.ContainsAny(f, "0123456789")
		if hasDigit || units[f] {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return "to taste", strings.Trim(line, ",")
	}
	return strings.Join(fields[:i], " "), strings.Trim(strings.Join(fields[i:], " "), ",")
}

// --- Stores ------------------------------------------------------------------

// FixtureStoreProvider synthesises plausible stores around the given
// coordinate. The names are generic chains and the offsets are real, so the
// distances and map links behave correctly as the location changes.
type FixtureStoreProvider struct{}

func NewFixtureStoreProvider() *FixtureStoreProvider { return &FixtureStoreProvider{} }

type fixtureStore struct {
	name   string
	street string
	dLatKm float64
	dLngKm float64
}

var fixtureStores = []fixtureStore{
	{"Whole Foods Market", "Market Street", 0.4, 0.3},
	{"Trader Joe's", "Union Avenue", -0.6, 0.5},
	{"Safeway", "Mission Boulevard", 1.1, -0.7},
	{"Kroger", "Cedar Lane", -1.3, -0.9},
	{"Sprouts Farmers Market", "Oak Street", 1.8, 1.2},
	{"Costco Wholesale", "Industrial Parkway", -2.4, 1.9},
	{"ALDI", "Highland Road", 2.7, -2.1},
	{"Local Co-op Grocery", "Elm Street", -0.2, -0.4},
}

func (f *FixtureStoreProvider) FindNearbyStores(_ context.Context, lat, lng float64) ([]models.Store, error) {
	const kmPerDegLat = 110.574
	kmPerDegLng := 111.320 * math.Cos(lat*math.Pi/180)
	if kmPerDegLng == 0 {
		kmPerDegLng = 111.320
	}

	type scored struct {
		store models.Store
		dist  float64
	}
	scoredStores := make([]scored, 0, len(fixtureStores))
	for i, s := range fixtureStores {
		sLat := lat + s.dLatKm/kmPerDegLat
		sLng := lng + s.dLngKm/kmPerDegLng
		distance := math.Sqrt(s.dLatKm*s.dLatKm + s.dLngKm*s.dLngKm)

		scoredStores = append(scoredStores, scored{
			store: models.Store{
				Name:     s.name,
				Address:  fmt.Sprintf("%d %s", 100+i*137%900, s.street),
				Distance: fmt.Sprintf("%.1f km", distance),
				// A real maps link for the synthesised coordinate, so the
				// button actually goes somewhere.
				MapURL: fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%.6f,%.6f", sLat, sLng),
			},
			dist: distance,
		})
	}

	// Nearest first. A list headed "where to buy" that opens with the store
	// 3km away and buries the one 400m away is answering the wrong question.
	sort.Slice(scoredStores, func(i, j int) bool { return scoredStores[i].dist < scoredStores[j].dist })

	stores := make([]models.Store, 0, len(scoredStores))
	for _, s := range scoredStores {
		stores = append(stores, s.store)
	}
	return stores, nil
}
