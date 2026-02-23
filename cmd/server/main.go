package main

import (
	api "mini-database/API"
	"mini-database/engine"
)

func main() {
	e := engine.NewEngine()
	server := &api.Server{Engine: e}
	server.Start(":8080")
}
