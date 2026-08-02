package models

type Store struct {
	Name     string `json:"name"`
	Address  string `json:"address"`
	Distance string `json:"distance"`
	MapURL   string `json:"mapUrl"`
}

type RecipeResponse struct {
	Ingredients []Ingredient `json:"ingredients"`
	Stores      []Store      `json:"stores"`
}

type Ingredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
}
