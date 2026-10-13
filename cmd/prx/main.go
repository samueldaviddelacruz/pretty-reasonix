// Command prx is the entry point for pretty-reasonix.
//
// It does nothing but hand the arguments to the CLI layer: everything that
// decides behaviour, output or an exit code lives in internal/frontend/cli, so
// this file stays small enough to audit at a glance. Only cmd/ and
// internal/frontend/ may write to the process streams or call os.Exit
// (docs/TECHNICAL_DESIGN.md §6.2), which is why the streams are passed down
// rather than reaching for os.Stdout here.
package main

import (
	"os"

	"pretty-reasonix/internal/frontend/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
