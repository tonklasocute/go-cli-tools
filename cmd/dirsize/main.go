// Command dirsize is a concurrent directory size analyzer.
package main

import (
	"os"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/dirsize"
)

func main() { os.Exit(cliutil.Main(dirsize.NewCommand)) }
