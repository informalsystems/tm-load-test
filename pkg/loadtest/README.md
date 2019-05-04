# Tendermint Load Testing Framework

By default, `tm-load-test` is built using a client that makes use of the native
Tendermint RPC client to interact with a Tendermint network running the
`kvstore` proxy application.

If, however, you want to customize the load testing client for your own ABCI
application, you can do so by implementing the `clients.ClientType`,
`clients.Factory` and `clients.Client` interfaces. An example of this is the
provided Tendermint RPC-based client factory and client
[here](./clients/http.go).

## Interactions
At a high level, a client executes **interactions**, where a single interaction
is made up of a series of requests. For the provided `kvstore-http` application,
the two requests that make up an interaction are:

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
    "github.com/interchainio/tm-load-test/pkg/loadtest/clients"
)

func init() {
    // Assumes newXYZClientType() is a function that instantiates your struct
    // that implements clients.ClientType
    clients.RegisterClientType("xyz-client", newXYZClientType())
}

func newXYZClientType() clients.ClientType {
    // implement your client type instantiation here
    // ...
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
# standard Go build
GO111MODULE=on go build -o build/xyz-load-test ./main.go
```

It can then be executed with the same parameters and configuration file as the
standard [`tm-load-test`](../../cmd/tm-load-test/README.md) application. The
primary difference is that you would need to set your client type in the
`[clients]` section of your `config.toml` file:

```toml
# config.toml
# ...

[clients]
type = "xyz-client"

# ...
```

Then run:

```bash
# each in a separate terminal
./build/xyz-load-test -c config.toml -master
./build/xyz-load-test -c config.toml -slave
./build/xyz-load-test -c config.toml -slave
# ...
```

## Testing Your Application
See [here](./integration_test.go) for an example as to how to go about
implementing a simple integration test that runs a full Tendermint node and
executes a load test against it.
