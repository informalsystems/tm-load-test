[![CircleCI](https://circleci.com/gh/interchainio/tm-load-test/tree/master.svg?style=svg)](https://circleci.com/gh/interchainio/tm-load-test/tree/master)

# tm-load-test

`tm-load-test` is a distributed load testing tool (and framework) for load
testing [Tendermint](https://tendermint.com/) networks.

## Requirements
In order to build and use the tools, you will need:

* Go 1.12+
* `make`

## Building
To build the `tm-load-test` binary in the `build` directory:

```bash
make
```

## Usage

`tm-load-test` requires the use of a TOML-based configuration file to execute a
load test. See the [examples](./examples/) folder for some examples, where the
comments in each example explain what each parameter means.

This application can operate in either **standalone**, or **master/slave** mode.

### Standalone Mode
By default, `tm-load-test` is a distributed load testing tool, but to run a load
test locally (which internally creates a single master and slave node, ignoring
the `master.expect_slaves` and `slave.master` parameters in your configuration
file):

```bash
# Initialize your local Tendermint node to ~/.tendermint
tendermint init
# Run a node with the kvstore proxy app
tendermint node --proxy_app kvstore

# Run the load test in standalone mode with the given configuration (-v sets
# output logging to DEBUG level)
./build/tm-load-test -c examples/load-test.toml -mode standalone -v
```

### Master/Slave Mode
The [`load-test.toml`](./examples/load-test.toml) example demonstrates usage
with the following configuration:

* A single Tendermint node with RPC endpoint available at `localhost:26657`
* The load testing master bound to `localhost:26670`
* 2 slaves bound to arbitrary ports on `localhost`
* Each slave spawns 50 clients
* Each client executes 100 interactions with the Tendermint node

To run the example test, simply do the following from the folder into which you
cloned the `tm-load-test` source:

```bash
# Initialize your local Tendermint node to ~/.tendermint
tendermint init
# Run a node with the kvstore proxy app
tendermint node --proxy_app kvstore

# Run each of the following in a separate terminal (-v sets output logging to
# DEBUG level)
./build/tm-load-test -c examples/load-test.toml -mode master -v
./build/tm-load-test -c examples/load-test.toml -mode slave -v
./build/tm-load-test -c examples/load-test.toml -mode slave -v
```

And then watch the output logs to see the load testing progress.

Alternatively, there is a `run.sh` script provided in the `examples` folder that
will help with executing load tests where 1 master and 2 slaves are required. To
run the examples for this, simply do the following:

```bash
# For a fresh Tendermint setup
tendermint init
tendermint node --proxy_app kvstore

# Runs 2 slaves in the background and the master in the foreground, so you can
# easily kill the load test (Ctrl+C)
./examples/run.sh examples/load-test.toml
```

## Load Testing Clients
There are 2 [clients](./pkg/loadtest/clients/) provided at present, both of
which require the `kvstore` or `persistent_kvstore` ABCI apps running on your
Tendermint network:

* `kvstore-http` - Allows for load testing via the standard Tendermint RPC
  client (which interacts over HTTP, with a separate HTTP request for each
  interaction).
* `kvstore-websockets` - Allows for load testing via the Tendermint WebSockets
  RPC interface. Note that this requires the use of Tendermint's event
  subscription subsystem, which means that you can have a maximum of 99 clients
  per Tendermint node before your slaves will start failing. Each spawned load
  testing client will create a separate WebSockets connection to a single random
  target node.

See the [examples](./examples/) folder for examples of load testing
configuration files that make use of each of these client types.

### Customizing
To implement your own client type to load test your own Tendermint ABCI
application, see the [`loadtest` package docs here](./pkg/loadtest/README.md).

## Outage Simulation
See the [`tm-outage-sim-server`](./cmd/tm-outage-sim-server/) folder for
documentation regarding the controlled simulation of Tendermint node "outages".

## Development
To run the linter and the tests:

```bash
make lint
make test
```

