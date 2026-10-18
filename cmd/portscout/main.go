// Command portscout is a fast concurrent TCP port scanner for dev machines.
package main

import (
	"os"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/portscout"
)

func main() { os.Exit(cliutil.Main(portscout.NewCommand)) }
