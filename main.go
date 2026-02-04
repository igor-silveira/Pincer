package main

import (
	"os"

	"github.com/igorsilveira/pincer/cmd/pincer"
)

func main() {
	if err := pincer.Execute(); err != nil {
		os.Exit(1)
	}
}
