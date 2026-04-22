package main

import (
	"os"

	"github.com/Anthoooooooony/gw/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
