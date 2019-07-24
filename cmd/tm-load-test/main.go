package main

import (
	"github.com/interchainio/tm-load-test/pkg/loadtest"
)

const appLongDesc = `
Load testing application for Tendermint with optional master/slave mode.
Generates large quantities of arbitrary transactions and submits those 
transactions to one or more Tendermint endpoints. By default, it assumes that
you are running the kvstore ABCI application on your Tendermint network.

To run the application in a similar fashion to tm-bench (STANDALONE mode):
    tm-load-test -c 1 -T 10 -r 1000 -s 250 \
        --broadcast-tx-method async \
        --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket

To run the application in MASTER mode:
    tm-load-test \
        master \
        --expect-slaves 2 \
        --bind localhost:26670 \
        -c 1 -T 10 -r 1000 -s 250 \
        --broadcast-tx-method async \
        --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket

To run the application in SLAVE mode:
    tm-load-test slave --master localhost:26680

NOTE: In SLAVE mode, all load testing-related flags are ignored. The slave
always takes instructions from the master node it's connected to.
`

func main() {
	loadtest.Run(&loadtest.CLIConfig{
		AppName:              "tm-load-test",
		AppShortDesc:         "Load testing application for Tendermint kvstore",
		AppLongDesc:          appLongDesc,
		DefaultClientFactory: "kvstore",
	})
}
