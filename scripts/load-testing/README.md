# Remote Testing

This folder will eventually contain a number of recipes for deploying remote
test networks for debugging and testing various different Tendermint network
configurations and scenarios. The primary tool for deploying these various
scenarios is [Ansible](https://docs.ansible.com/ansible/latest/).

## Folder layout
Following is a description of this folder's layout:

```
|_ common/           Common `make` and Ansible includes
|_ experiments/      Result data and charts from scenario executions will be stored here
|_ inventory/        Where to store all of your Ansible host inventory files
|_ networks/         The different remote network configurations
|_ scenarios/        The different testing scenarios from the client side
|_ Makefile          The primary Makefile for executing the different network deployments and scenarios
```

## Requirements
In order to execute the various deployments or scenarios, you will need:

* `make`
* Python 3.6+
* Tendermint [development
  requirements](https://github.com/tendermint/tendermint#minimum-requirements)
  (for building the Tendermint binary that we deploy to the test networks)

Target platform for execution of these deployments/scenarios is either
Linux/macOS.

## Load Testing Guide
See the [Load Testing Guide](GUIDE.md) for a step-by-step guide to setting up
and executing your own load tests against a Tendermint network using these
scripts.

## Managing Python dependencies
By default, the first time you execute a deployment or a scenario, a Python 3
virtual environment will be created in the `testing/venv` folder in this repo. A
few dependencies will also automatically be downloaded and installed into this
virtual environment.

It's probably a good idea, however, to update these dependencies (if the
`requirements.txt` changes):

```bash
# Update Python virtual environment and dependencies
make update_deps
```

## Deploying test networks
To deploy a particular test network to the relevant hosts, simply do the
following:

```bash
make deploy:001-reference
```

Each network deployment potentially has a different set of parameters that one
can supply via environment variables. See each network's folder for details.

The following test network configurations are available for deployment:

* [`001-reference`](./networks/001-reference/README.md) - A simple reference Tendermint
  network, running `kvstore` with `create_empty_blocks=true`.

## Executing test scenarios
To execute a particular testing scenario, simply:

```bash
make scenario:001-kvstore-test
```

This particular test scenario assumes you're running the `kvstore` proxy app in
your Tendermint network.

The following testing scenarios are currently provided:

* [`001-kvstore-test`](./scenarios/001-kvstore-test/README.md) - Simple `kvstore` test,
  which stores a random value in a particular node and attempts to read it back
  out.
* [`002-kvstore-loadtest`](./scenarios/002-kvstore-loadtest/README.md) - A load test using
  [Locust](https://locust.io) to be executed from a single machine. The
  assumption here is that the target Tendermint network runs `kvstore` as its
  proxy app.
* [`003-kvstore-loadtest-distributed`](./scenarios/003-kvstore-loadtest-distributed/README.md)
  - A distributed load test (multiple source machines) using Locust.
* [`004-kvstore-loadtest-distcollection`](./scenarios/004-kvstore-loadtest-distcollection/README.md)
  - Runs a suite/collection of distributed load tests (i.e.
    `003-kvstore-loadtest-distributed`) with varying parameters (number of
    clients, hatch rate, run time, etc.).
