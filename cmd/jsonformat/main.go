// Command jsonformat pretty-prints, minifies, validates and queries JSON.
package main

import (
	"os"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/jsonformat"
)

func main() { os.Exit(cliutil.Main(jsonformat.NewCommand)) }
