package main

import (
	"os"

	"github.com/docker/cagent/cmd/root"
)

func main() {
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
