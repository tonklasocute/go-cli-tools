// Command envcheck validates .env files against an annotated .env.example schema.
package main

import (
	"os"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/envcheck"
)

func main() { os.Exit(cliutil.Main(envcheck.NewCommand)) }
