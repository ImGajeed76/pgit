package main

import (
	"os"

	"github.com/imgajeed76/pgit/v3/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
