package services

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Versioned is implemented by providers whose output depends on something that
// can change under a cached row — the model, or the prompt driving it.
//
// It is an optional interface rather than a method on IngredientProvider so
// that a caller can supply a bare provider (a test double, a future extractor)
// without being forced to reason about cache invalidation. A provider that does
// not implement it is treated as unversioned and its extractions are never
// reused across a restart.
type Versioned interface {
	ModelVersion() string
}

// UnversionedModel is the version recorded for a provider that cannot describe
// itself. It is deliberately not a valid cache key: nothing stored under it is
// ever read back.
const UnversionedModel = "unversioned"

// fingerprintLength is how much of the prompt digest is kept. Six bytes is
// short enough to read in a log line and long enough that two prompts will not
// collide in any repository's lifetime.
const fingerprintLength = 12

// ModelVersion composes the model name with a fingerprint of the prompt.
//
// Cached extractions are permanent, so the cache key has to name everything
// that decides the output. The model alone does not: editing the prompt to ask
// for units, or to drop the "ignore sponsorships" rule, changes every list the
// same model produces. Without the fingerprint that edit would ship and the
// service would keep serving output the current prompt would never generate.
func ModelVersion(model, prompt string) string {
	sum := sha256.Sum256([]byte(normalisePrompt(prompt)))
	return model + "@" + hex.EncodeToString(sum[:])[:fingerprintLength]
}

// normalisePrompt strips leading and trailing whitespace so that reindenting a
// raw string literal does not invalidate every stored extraction, while any
// change to the wording still does.
func normalisePrompt(prompt string) string {
	lines := strings.Split(prompt, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ProviderModelVersion reports how to version a provider's output, falling back
// to UnversionedModel for providers that do not describe themselves.
func ProviderModelVersion(p IngredientProvider) string {
	if v, ok := p.(Versioned); ok {
		if s := strings.TrimSpace(v.ModelVersion()); s != "" {
			return s
		}
	}
	return UnversionedModel
}
