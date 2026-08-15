package services

import (
	"context"
	"fmt"
	"strings"

	"market-mate/models"

	"github.com/sashabaranov/go-openai"
)

// IngredientExtractor is the live OpenAI implementation of IngredientProvider.
type IngredientExtractor struct {
	client *openai.Client
	model  string
}

func NewIngredientExtractor(apiKey string) *IngredientExtractor {
	return &IngredientExtractor{
		client: openai.NewClient(apiKey),
		model:  openai.GPT4oMini,
	}
}

const extractionPrompt = `Extract the cooking ingredients and their quantities from the recipe description below.

Rules:
- Output one ingredient per line, formatted exactly as: quantity | ingredient name
- If no quantity is given for an ingredient, use "to taste" as the quantity.
- Include only things that go into the dish. Ignore equipment, links, sponsorships, and chatter.
- If the description contains no ingredients at all, output nothing.
- Output no headers, numbering, commentary, or markdown.

Description:
%s`

// ExtractIngredients asks the model for a structured list.
//
// The delimiter is "|" rather than the previous "-": ingredient lines are full
// of hyphens ("sun-dried tomatoes", "1/2 cup half-and-half"), so splitting on
// the first hyphen mangled exactly the ingredients most likely to appear in a
// real recipe. Parsing falls back to the text parser when the model ignores the
// format, so prose degrades into a partial list instead of an error.
func (ie *IngredientExtractor) ExtractIngredients(ctx context.Context, description string) ([]models.Ingredient, error) {
	if strings.TrimSpace(description) == "" {
		return []models.Ingredient{}, nil
	}

	resp, err := ie.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       ie.model,
		Temperature: 0, // deterministic: the same video should give the same list
		Messages: []openai.ChatCompletionMessage{{
			Role:    openai.ChatMessageRoleUser,
			Content: fmt.Sprintf(extractionPrompt, description),
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}
	if len(resp.Choices) == 0 {
		return []models.Ingredient{}, nil
	}

	ingredients := parseDelimited(resp.Choices[0].Message.Content)
	if len(ingredients) == 0 {
		// The model answered in prose. Salvage what we can rather than
		// reporting an empty recipe for a video that clearly has one.
		ingredients = parseIngredientLines(resp.Choices[0].Message.Content)
	}
	return ingredients, nil
}

// parseDelimited reads the "quantity | name" format the prompt asks for.
func parseDelimited(content string) []models.Ingredient {
	out := []models.Ingredient{}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimLeft(line, "-*•·0123456789. \t")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		quantity := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		if quantity == "" {
			quantity = "to taste"
		}
		out = append(out, models.Ingredient{Name: name, Quantity: quantity})
	}
	return out
}
