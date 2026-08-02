package handlers

import (
	"market-mate/models"
	"market-mate/services"
	"net/http"

	"github.com/gin-gonic/gin"
)

type VideoHandlerConfig struct {
	VideoService        *services.VideoService
	StoreFinder         *services.StoreFinder
	IngredientExtractor *services.IngredientExtractor
	CacheService        *services.CacheService
	LocationService     *services.LocationService
}

type VideoHandler struct {
	config VideoHandlerConfig
}

func NewVideoHandler(cfg VideoHandlerConfig) *VideoHandler {
	return &VideoHandler{
		config: cfg,
	}
}

func (h *VideoHandler) ProcessVideo(c *gin.Context) {
	var request struct {
		URL string `json:"url"`
	}

	if err := c.BindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	videoID := services.ExtractVideoID(request.URL)
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid YouTube URL"})
		return
	}

	video, err := h.config.VideoService.GetVideoDetails(videoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Extract ingredients from video description using AI
	ingredients, err := h.config.IngredientExtractor.ExtractIngredients(video.Snippet.Description)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to extract ingredients: " + err.Error()})
		return
	}

	// For demo purposes, using fixed location (San Francisco)
	stores, err := h.config.StoreFinder.FindNearbyStores(37.7749, -122.4194)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := models.RecipeResponse{
		Ingredients: ingredients,
		Stores:      stores,
	}

	c.JSON(http.StatusOK, response)
}
