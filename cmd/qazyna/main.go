package main

import (
	"fmt"
	"os"

	"qazyna/internal/cli"
)

func main() {
	err := cli.Run(os.Args,
		cli.WithDefaultStore(),
		cli.WithDefaultParsers(),
		cli.WithDefaultEmbedder(),
		cli.WithFakeEmbedder(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
