//go:build ignore

// fakeports opens TCP listeners on the given ports so the portscout demo has
// something to find. Usage: go run docs/tapes/fakeports.go 5173 5432 6379 8080
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
)

func main() {
	for _, p := range os.Args[1:] {
		ln, err := net.Listen("tcp", "127.0.0.1:"+p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
}
