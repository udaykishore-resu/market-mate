package services

import (
	"context"
	"fmt"
	"market-mate/models"

	"strings"

	"github.com/sashabaranov/go-openai"
)

type IngredientExtractor struct {
	client *openai.Client
}

func NewIngredientExtractor(apiKey string) *IngredientExtractor {
	return &IngredientExtractor{
		client: openai.NewClient(apiKey),
	}
}

func (ie *IngredientExtractor) ExtractIngredients(description string) ([]models.Ingredient, error) {
	prompt := fmt.Sprintf(`
Extract cooking ingredients and their quantities from the following recipe description. 
Format each ingredient as "quantity - ingredient". If no quantity is specified, use "to taste".

Description:
%s

Return only the list of ingredients, one per line.`, description)

	resp, err := ie.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT4,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %v", err)
	}

	// Parse the response into ingredients
	ingredientLines := strings.Split(resp.Choices[0].Message.Content, "\n")
	var ingredients []models.Ingredient

	for _, line := range ingredientLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "-", 2)
		if len(parts) != 2 {
			continue
		}

		ingredients = append(ingredients, models.Ingredient{
			Quantity: strings.TrimSpace(parts[0]),
			Name:     strings.TrimSpace(parts[1]),
		})
	}

	return ingredients, nil
}
