package main

import (
	"log"
	"log/slog"
	"net/http"

	"github.com/opensuperapp/opensuperapp/backend-services/core/internal/config"
	"github.com/opensuperapp/opensuperapp/backend-services/core/internal/database"
	"github.com/opensuperapp/opensuperapp/backend-services/core/internal/router"

	_ "github.com/opensuperapp/opensuperapp/backend-services/core/plugins"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Connect to the database
	db := database.Connect(cfg)
	defer database.Close(db)

	// Initialize HTTP routes
	mux := router.NewRouter(db, cfg)

	// Start the server
	slog.Info("Starting server", "port", cfg.ServerPort)
	log.Fatal(http.ListenAndServe(":"+cfg.ServerPort, mux))
}
