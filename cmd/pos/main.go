package main

import (
	"fmt"
	"mini-database/cmd/internal/cli"
	"os"
)

func main() {
	// Set up deferred cleanup
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", r)
			os.Exit(1)
		}
	}()

	cli.Execute()
}
