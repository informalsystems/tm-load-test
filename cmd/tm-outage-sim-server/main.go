package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/informalsystems/tm-load-test/internal/outagesim"
)

func main() {
	var (
		addr         = flag.String("addr", ":26680", "the address to which to bind this server")
		username     = flag.String("u", "tm-load-test", "the username for requests authenticating against the control endpoint")
		passwordHash = flag.String("p", "", "a bcrypt password hash for authenticating requests to the control endpoint")
	)
	flag.Usage = func() {
		fmt.Println(`Tendermint outage simulator server

Provides an authenticated HTTP interface through which one can bring a local
Tendermint service down or up.

This application requires the relevant system privileges to be able to interact
with the Tendermint service. See https://unix.stackexchange.com/q/215412 for an
example as to how to configure a non-root user to have the relevant sudo
privileges to start/stop a service.

Usage:
  tm-outage-sim-server \
    -addr 127.0.0.1:26680 \
    -u tm-load-test \
    -p "\$2a\$12\$ac6f8zq9vvugNb3QXeOV9.RGFHeu8a7qhf9WIRAfH69a0k2j7J7wy"

Note the use of escape characters above when specifying the bcrypt hash on the
command line. Alternatively, you can replace the dollar signs ($) with
hash symbols (#), such as:

  tm-outage-sim-server \
    -addr 127.0.0.1:26680 \
    -u tm-load-test \
    -p "#2a#12#ac6f8zq9vvugNb3QXeOV9.RGFHeu8a7qhf9WIRAfH69a0k2j7J7wy"

Flags:`)
		flag.PrintDefaults()
		fmt.Println(`
Examples of how to bring Tendermint up/down:
  curl -s -X POST -d "up" http://tm-load-test:testpassword@127.0.0.1:26680
  curl -s -X POST -d "down" http://tm-load-test:testpassword@127.0.0.1:26680`)
		fmt.Println("")
	}
	flag.Parse()

	if *username == "" {
		fmt.Println("Error: a username is required for running this service")
		os.Exit(1)
	}
	if *passwordHash == "" {
		fmt.Println("Error: a bcrypt password hash is required for running this service")
		os.Exit(1)
	}

	http.HandleFunc(
		"/",
		outagesim.MakeOutageEndpointHandler(
			*username,
			strings.Replace(*passwordHash, "#", "$", -1),
			outagesim.IsTendermintRunning,
			outagesim.ExecuteServiceCmd,
		),
	)
	log.Printf("Starting Tendermint outage simulator server at %s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
