package main

import (
	"os"

	"github.com/shamith/mongodiff/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
