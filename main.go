package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/docker/cagent/cmd/root"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	if err := root.Execute(ctx, os.Stdin, os.Stdout, os.Stderr, os.Args[1:]...); err != nil {
		cancel()
		os.Exit(1)
	} else {
		cancel()
		os.Exit(0)
	}
}
