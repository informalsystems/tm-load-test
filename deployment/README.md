# Common Load Testing Deployment Scenarios

This folder contains two sets of scripts:

1. [An example](./tendermint-testnet.md), using Ansible, of deploying a
   multi-node Tendermint test network to a group of remote machines (e.g. VMs),
   alongside the [outage simulator server](../cmd/tm-outage-sim-server/), in
   order to prepare a load testing operation for it.
2. A set of scripts to execute a load testing scenario against the target nodes.
   This set of scripts contains two kinds of load testing scenarios:
   1. A locally driven load testing scenario, where `tm-load-test` is used from
      one's local machine to send transactions to a deployed Tendermint network.
   2. A remotely driven load testing scenario, where one uses Ansible scripts to
      deploy `tm-load-test` across multiple machines to target the Tendermint
      test network.
