// Command app is the fixture binary.
package main

import (
	"flag"
	"fmt"
	"os"

	"example.com/fixture/pkg/httpx"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	verbose := flag.Bool("verbose", false, "verbose output")
	var config string
	flag.StringVar(&config, "config", "config.json", "config file")
	flag.Parse()

	if os.Getenv("FIXTURE_DEBUG") != "" {
		*verbose = true
	}
	if v, ok := os.LookupEnv("FIXTURE_TOKEN"); ok && v == "" {
		fmt.Fprintln(os.Stderr, "empty token")
	}
	s := httpx.NewServer(*addr)
	fmt.Println(s.Addr(), config, *verbose)
}
