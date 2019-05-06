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
There is an example load testing configuration file in the `examples` folder.
This example demonstrates usage with the following configuration:

* A single Tendermint node with RPC endpoint available at `localhost:26657`
* The load testing master bound to `localhost:35000`
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
./build/tm-load-test -c examples/load-test.toml -master -v
./build/tm-load-test -c examples/load-test.toml -slave -v
./build/tm-load-test -c examples/load-test.toml -slave -v
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

## Development
To run the linter and the tests:

```bash
make lint
make test
```

## Scripts
`tm-load-test` is tedious to use across many machines without some form of
automation, so to help along those lines there are
[Ansible](https://docs.ansible.com/ansible/latest/index.html) scripts in the
[`scripts/load-testing`](./scripts/load-testing/README.md) folder.
