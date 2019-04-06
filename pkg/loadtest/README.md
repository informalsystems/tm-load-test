# Tendermint Load Testing Framework

By default, `tm-load-test` is built using a client that makes use of the native
Tendermint RPC client to interact with a Tendermint network running the
`kvstore` application.

If, however, you want to customize the load testing client for your own ABCI
application, you can do so by implementing the `loadtest.ClientFactory` and
`loadtest.Client` interfaces. An example of this is the provided Tendermint
RPC-based client factory and client [here](./client.go#148).

## Interactions
At a high level, a client executes **interactions**, where a single interaction
is made up of a series of requests. For the provided `kvstore` application, the
two requests that make up an interaction are:

1. Put a value into the `kvstore` by way of a `broadcast_tx_sync` request. This
   request is directed at a random Tendermint node in the network.
2. Retrieve the value from the `kvstore` for the original key and make sure that
   the retrieved value is the same as the stored value. This request is also
   directed at a (potentially different) random node in the Tendermint network.

You can implement your own complex scenarios for your own application within an
"interaction".

## Custom Client Project Setup
To create your own load testing tool for your environment, you will need to
create your own equivalent of the `tm-load-test` command, but register your
client during startup for use from the configuration file.

```go
// my-tm-load-test.go
package main

import (
    "github.com/interchainio/tm-load-test/pkg/loadtest"
)

func init() {
    // Register your custom client factory here
    loadtest.RegisterClientFactory("myclient", &MyCustomClientFactory{})
}

func main() {
    // This will parse command line flags, configuration, etc. and actually
    // execute the load testing framework in either master or slave mode,
    // depending on the command line parameters supplied.
    loadtest.Run()
}
```

## Building and Running Your Load Test Application
Then, produce your own executable for your load testing application:

```bash
go build -o build/my-tm-load-test ./path/to/my-tm-load-test.go
```

It can then be executed with the same parameters and configuration file as the
standard [`tm-load-test`](../../cmd/tm-load-test/README.md) application.
