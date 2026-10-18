// Command reqping checks HTTP(S) endpoints and reports status, latency and TLS expiry.
package main

import (
	"os"

	"github.com/tonklasocute/go-cli-tools/internal/cliutil"
	"github.com/tonklasocute/go-cli-tools/internal/reqping"
)

func main() { os.Exit(cliutil.Main(reqping.NewCommand)) }
