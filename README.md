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
tendermint node \
    --proxy_app kvstore \
    --consensus.create_empty_blocks=false

# Run each of the following in a separate terminal (-v sets output logging to
# DEBUG level)
./build/tm-load-test -c examples/load-test.toml -master -v
./build/tm-load-test -c examples/load-test.toml -slave -v
./build/tm-load-test -c examples/load-test.toml -slave -v
```

And then watch the output logs to see the load testing progress.

## Customizing
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
