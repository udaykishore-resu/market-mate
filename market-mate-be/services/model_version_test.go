package services

import (
	"context"
	"strings"
	"testing"

	"market-mate/models"
)

// TestModelVersion covers the invariant the permanent cache rests on: the
// version string changes when, and only when, something that changes the output
// changes.
func TestModelVersion(t *testing.T) {
	const prompt = "Extract the ingredients.\nOne per line."

	cases := []struct {
		name        string
		model       string
		prompt      string
		wantSameAs  string
		wantDiffers bool
	}{
		{name: "baseline", model: "gpt-4o-mini", prompt: prompt},
		{name: "same inputs are stable", model: "gpt-4o-mini", prompt: prompt, wantSameAs: "baseline"},
		{name: "reindented prompt is the same prompt", model: "gpt-4o-mini",
			prompt: "  \nExtract the ingredients.\nOne per line.\n\n", wantSameAs: "baseline"},
		{name: "trailing spaces do not count", model: "gpt-4o-mini",
			prompt: "Extract the ingredients.   \nOne per line.", wantSameAs: "baseline"},
		{name: "a reworded prompt invalidates", model: "gpt-4o-mini",
			prompt: "Extract the ingredients and their units.\nOne per line.", wantDiffers: true},
		{name: "one changed character invalidates", model: "gpt-4o-mini",
			prompt: "Extract the ingredient.\nOne per line.", wantDiffers: true},
		{name: "a different model invalidates", model: "gpt-4o", prompt: prompt, wantDiffers: true},
	}

	versions := map[string]string{}
	baseline := ModelVersion("gpt-4o-mini", prompt)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ModelVersion(tc.model, tc.prompt)
			versions[tc.name] = got

			if tc.wantSameAs != "" && got != baseline {
				t.Errorf("ModelVersion = %q, want the baseline %q", got, baseline)
			}
			if tc.wantDiffers && got == baseline {
				t.Errorf("ModelVersion = %q, which must not equal the baseline", got)
			}

			// The model name has to stay readable: this string lands in a log
			// line and a database column an operator has to interpret.
			if !strings.HasPrefix(got, tc.model+"@") {
				t.Errorf("ModelVersion = %q, want it to start with %q", got, tc.model+"@")
			}
			if fingerprint := strings.TrimPrefix(got, tc.model+"@"); len(fingerprint) != fingerprintLength {
				t.Errorf("fingerprint %q is %d chars, want %d", fingerprint, len(fingerprint), fingerprintLength)
			}
		})
	}
}

func TestProviderModelVersion(t *testing.T) {
	cases := []struct {
		name     string
		provider IngredientProvider
		want     string
		prefix   string
	}{
		{
			name:     "fixture provider versions itself against its catalogue",
			provider: NewFixtureIngredientProvider(),
			prefix:   models.SourceFixture + "@",
		},
		{
			name:     "live extractor versions itself against model and prompt",
			provider: NewIngredientExtractor("sk-not-a-real-key"),
			prefix:   "gpt-4o-mini@",
		},
		{
			name:     "a provider that cannot describe itself is never cached",
			provider: unversionedProvider{},
			want:     UnversionedModel,
		},
		{
			name:     "an empty version is treated as unversioned",
			provider: blankVersionProvider{},
			want:     UnversionedModel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProviderModelVersion(tc.provider)
			if tc.want != "" && got != tc.want {
				t.Errorf("ProviderModelVersion = %q, want %q", got, tc.want)
			}
			if tc.prefix != "" && !strings.HasPrefix(got, tc.prefix) {
				t.Errorf("ProviderModelVersion = %q, want prefix %q", got, tc.prefix)
			}
		})
	}
}

// TestFixtureAndLiveVersionsCannotCollide is a provenance guarantee, not a
// hygiene one: if the two namespaces could overlap, a demo extraction stored in
// Postgres could be read back to answer a request running against live keys.
func TestFixtureAndLiveVersionsCannotCollide(t *testing.T) {
	fixture := ProviderModelVersion(NewFixtureIngredientProvider())
	live := ProviderModelVersion(NewIngredientExtractor("sk-not-a-real-key"))

	if fixture == live {
		t.Fatalf("fixture and live share the model version %q", fixture)
	}
	if !strings.HasPrefix(fixture, models.SourceFixture+"@") {
		t.Errorf("fixture version %q does not identify itself as a fixture", fixture)
	}
	if strings.HasPrefix(live, models.SourceFixture+"@") {
		t.Errorf("live version %q claims to be a fixture", live)
	}
}

type unversionedProvider struct{}

func (unversionedProvider) ExtractIngredients(context.Context, string) ([]models.Ingredient, error) {
	return nil, nil
}

type blankVersionProvider struct{ unversionedProvider }

func (blankVersionProvider) ModelVersion() string { return "  " }
