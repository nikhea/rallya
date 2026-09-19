package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"app":    "Rallya API",
		})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		api.GET("/hello", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "Welcome to Rallya",
			})
		})
	}

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	log.Println("Rallya API running on :8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}