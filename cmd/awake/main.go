// Command awake keeps a computer awake for bounded, observable,
// user-controlled sessions.
//
// This file is the composition root: it is the only place that knows which
// concrete implementations are in play. Everything it builds is handed to the
// CLI, which is a thin client over the application core (ADR-0001).
package main

import (
	"os"

	"github.com/javiervargas02/awake/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
