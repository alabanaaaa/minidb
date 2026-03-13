package main

import (
	"fmt"
	api "mini-database/API"
	"mini-database/engine"
	"os"
	"path/filepath"
)

func main() {
	// Ensure data directory exists
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Println("failed to create data dir:", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(dataDir, "storage.db")

	e, err := engine.NewEngineWithDB(dbPath)
	if err != nil {
		fmt.Println("failed to start engine with DB, falling back to in-memory:", err)
		e = engine.NewEngine()
	}

	server := &api.Server{Engine: e}

	if err := server.Start(":8080"); err != nil {
		fmt.Println("server exited:", err)
		os.Exit(1)
	}
}
