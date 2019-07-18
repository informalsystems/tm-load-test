package main

import (
	"github.com/interchainio/tm-load-test/pkg/loadtest"
)

const appLongDesc = `Load testing application for Tendermint kvstore with optional master/slave mode.
Generates large quantities of arbitrary transactions and submits those 
transactions to one or more Tendermint endpoints. Assumes the kvstore ABCI app
to have been deployed on the target Tendermint network. For testing other kinds
of ABCI apps, you will need to build your own load testing client. See
https://github.com/interchainio/tm-load-test for details.

To run the application in a similar fashion to tm-bench (STANDALONE mode):
    tm-load-test -c 1 -T 10 -r 1000 -s 250 \
        --broadcast-tx-method async \
        --endpoints tm-endpoint1.somewhere.com:26657,tm-endpoint2.somewhere.com:26657

To run the application in MASTER mode:
    tm-load-test \
        master \
        --expect-slaves 2 \
        --bind localhost:26670 \
        -c 1 -T 10 -r 1000 -s 250 \
        --broadcast-tx-method async \
        --endpoints tm-endpoint1.somewhere.com:26657,tm-endpoint2.somewhere.com:26657 \

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
