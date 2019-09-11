# Changelog

## v0.6.0
* [\#33](https://github.com/interchainio/tm-load-test/pull/33) - Add ability
  to wait for a minimum level of connectivity between peers before starting the
  load testing (through a `--min-peer-connectivity` command line switch).

## v0.5.1
* [\#31](https://github.com/interchainio/tm-load-test/pull/31) - Expand on
  endpoint selection strategy to now allow for 3 different strategies:
  `supplied`, `discovered` and `any`. Allows for specifying of a seed node
  endpoint that one doesn't want to use during the actual load testing.

## v0.5.0
* [\#23](https://github.com/interchainio/tm-load-test/pull/23) - Add
  feature to wait for Tendermint network stabilization before starting load
  testing (in standalone and master/slave modes).
* [\#28](https://github.com/interchainio/tm-load-test/pull/28) - Expose the
  tx throughput rates via Prometheus.

## v0.4.2
* Adds version sub-command to the CLI

## v0.4.1
* [\#21](https://github.com/interchainio/tm-load-test/pull/21) - Add support
  for exposing Prometheus-compatible metrics from the MASTER web server via
  the `/metrics` endpoint. This now provides simple high-level information
  about the overall load test, like number of transactions sent (overall, and
  per-slave), and the state of the master and each attached slave.

## v0.4.0
* [\#20](https://github.com/interchainio/tm-load-test/pull/20) - Significant
  refactor of the underlying codebase to radically simplify the code and make
  its usage easier and more closely aligned with `tm-bench`.

## v0.3.0
* [\#15](https://github.com/interchainio/tm-load-test/pull/14) - Add example
  configuration for outage simulation while load testing.
* [\#14](https://github.com/interchainio/tm-load-test/pull/14) - Add support for
  the outage simulator from the master node during load testing.
* [\#13](https://github.com/interchainio/tm-load-test/pull/13) - Add basic HTTP
  authentication to `tm-outage-sim-server` utility.
* [\#12](https://github.com/interchainio/tm-load-test/pull/12) - Add standalone
  mode to be able to run load testing tool locally in a simple way.
* [\#11](https://github.com/interchainio/tm-load-test/pull/11) - Allow for basic
  HTTP authentication between the master and slave nodes for additional
  security.
* [\#10](https://github.com/interchainio/tm-load-test/pull/10) - Allow clients
  to continuously send transactions until the maximum time limit is reached
  without having a limit imposed on the number of transactions.

## v0.2.0
* First alpha release.
* Refactored comms mechanism between master and slaves to use HTTP for more
  robust communication and to simplify.
* Added the WebSockets-based client for interacting with Tendermint nodes over
  WebSockets (`kvstore-websockets`).

## v0.1.0
* Initial release to configure release management.

